package images

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/records"
	recordsfiles "github.com/SgtFlame/foundation/backend/features/records/files"
)

// MaxImageBytes is the largest image kept, and the largest reference read.
const MaxImageBytes = 20 << 20

// Deps are what the Google type needs: the image provider (usage-recorded,
// choosing each call's key from its principal) and the file store.
type Deps struct {
	Images providers.Images
	Files  recordsfiles.Files
}

// Impl is image.google's implementation, its deps late-bound.
func Impl(deps func() Deps) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(adapter.Context, adapter.Connection) (adapter.Session, error) {
		return session{deps}, nil
	}}
}

type session struct{ deps func() Deps }

func (session) Close() error { return nil }

// GenerateArgs are generate's arguments.
type GenerateArgs struct {
	Prompt      string            `json:"prompt"`
	AspectRatio string            `json:"aspectRatio,omitempty"`
	Count       int               `json:"count,omitempty"`
	Style       string            `json:"style,omitempty"`
	Reference   []records.FileRef `json:"reference,omitempty"`
}

// Generated is one image made.
type Generated struct {
	File   records.FileRef `json:"file"`
	Prompt string          `json:"prompt"`
	Model  string          `json:"model"`
}

// GenerateResult is generate's result.
type GenerateResult struct {
	Images []Generated `json:"images"`
	Model  string      `json:"model"`
	Text   string      `json:"text,omitempty"`
}

func (s session) Call(ctx adapter.Context, op string, raw json.RawMessage) (json.RawMessage, error) {
	d := s.deps()
	if d.Images == nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "image generation is not available on this server")
	}
	switch op {
	case "check":
		return json.Marshal(map[string]any{"ok": true})
	case "generate":
	default:
		return nil, connectors.Errorf(connectors.CodeNotSupported, "image.google has no operation %q", op)
	}
	var a GenerateArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the arguments are not generate's: %v", err)
	}
	a.Prompt = strings.TrimSpace(a.Prompt)
	switch {
	case a.Prompt == "":
		return nil, connectors.Errorf(connectors.CodeInvalid, "the prompt is empty")
	case a.Count < 0 || a.Count > MaxCount:
		return nil, connectors.Errorf(connectors.CodeInvalid, "count is from 1 to %d", MaxCount)
	case a.AspectRatio != "" && !slices.Contains(AspectRatios, a.AspectRatio):
		return nil, connectors.Errorf(connectors.CodeInvalid, "%q is not one of %s", a.AspectRatio, strings.Join(AspectRatios, ", "))
	case d.Files == nil:
		return nil, connectors.Errorf(connectors.CodeUnavailable, "there is no file store to keep images in")
	}
	prompt := a.Prompt
	if style := strings.TrimSpace(a.Style); style != "" {
		prompt += ", in " + style
	}
	in := providers.ImageInput{Prompt: prompt, AspectRatio: a.AspectRatio, Count: max(a.Count, 1)}
	tenant, user := ctx.Tenant(), ctx.Args.Principal.UserID
	for _, ref := range a.Reference {
		img, err := reference(ctx, d.Files, ref.ID)
		if err != nil {
			return nil, err
		}
		in.References = append(in.References, img)
	}
	call := ctx.Args
	call.Meta = call.Meta.WithFeature("image")
	out, err := d.Images.GenerateImages(call.CallContext(), in)
	if err != nil {
		return nil, providerError(err)
	}
	if out.Refused || len(out.Images) == 0 {
		why := out.Reason
		if why == "" {
			why = "it made no image"
		}
		return nil, connectors.Errorf(connectors.CodeRefused, "the model declined to draw this (%s)", strings.ToLower(why))
	}
	result := GenerateResult{Model: out.Model, Text: out.Text}
	for i, img := range out.Images {
		name := fmt.Sprintf("image-%d%s", i+1, extension(img.ContentType))
		ref, err := d.Files.Put(ctx, tenant, user, name, img.ContentType, bytes.NewReader(img.Data), MaxImageBytes)
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeUnavailable, "the image cannot be stored: %v", err)
		}
		result.Images = append(result.Images, Generated{File: ref, Prompt: prompt, Model: out.Model})
	}
	return json.Marshal(result)
}

// reference reads a reference image the caller uploaded, or one the
// dispatcher vouches they may read.
func reference(ctx adapter.Context, files recordsfiles.Files, id string) (providers.Image, error) {
	uploader, ok, err := files.Uploader(ctx, ctx.Tenant(), id)
	if err != nil {
		return providers.Image{}, connectors.Errorf(connectors.CodeUnavailable, "the reference %s cannot be read: %v", id, err)
	}
	if !ok || (uploader != ctx.Args.Principal.UserID && !ctx.Vouched(id)) {
		return providers.Image{}, connectors.Errorf(connectors.CodeNotFound, "there is no reference file %s you may use", id)
	}
	r, ref, ok, err := files.Open(ctx, ctx.Tenant(), id)
	if err != nil || !ok {
		return providers.Image{}, connectors.Errorf(connectors.CodeNotFound, "there is no reference file %s", id)
	}
	defer r.Close()
	if !strings.HasPrefix(ref.ContentType, "image/") {
		return providers.Image{}, connectors.Errorf(connectors.CodeInvalid, "the reference %s is %s, not an image", id, ref.ContentType)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
	if err != nil {
		return providers.Image{}, connectors.Errorf(connectors.CodeUnavailable, "the reference %s cannot be read: %v", id, err)
	}
	if len(data) > MaxImageBytes {
		return providers.Image{}, connectors.Errorf(connectors.CodeInvalid, "the reference %s is over %d MB", id, MaxImageBytes>>20)
	}
	return providers.Image{Data: data, ContentType: ref.ContentType}, nil
}

// providerError turns a provider's failure into a connector error.
func providerError(err error) error {
	var pe *providers.Error
	if !errors.As(err, &pe) {
		return connectors.Errorf(connectors.CodeUnavailable, "%v", err)
	}
	switch {
	case pe.Busy || pe.Code == http.StatusTooManyRequests:
		return connectors.Errorf(connectors.CodeRateLimited, "%s", pe.Message)
	case pe.Code == http.StatusUnauthorized || pe.Code == http.StatusForbidden:
		return connectors.Errorf(connectors.CodeAuth, "%s", pe.Message)
	case pe.Code == http.StatusBadRequest || pe.Code == http.StatusNotFound:
		return connectors.Errorf(connectors.CodeInvalid, "%s", pe.Message)
	}
	return connectors.Errorf(connectors.CodeUnavailable, "%s", pe.Message)
}

func extension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}
