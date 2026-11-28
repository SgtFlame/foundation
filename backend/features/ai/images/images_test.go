package images_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/images"
	"github.com/SgtFlame/foundation/backend/features/ai/providers/gemini"
	aisettings "github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/usage"
)

// memFiles is a file store in memory.
type memFiles struct {
	mu    sync.Mutex
	files map[string]memFile
}

type memFile struct {
	ref  records.FileRef
	user uint64
	data []byte
}

func (m *memFiles) Put(_ context.Context, _, user uint64, name, contentType string, r io.Reader, _ int64) (records.FileRef, error) {
	data, _ := io.ReadAll(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	ref := records.FileRef{ID: fmt.Sprintf("f%d", len(m.files)+1), Name: name, Size: int64(len(data)), ContentType: contentType}
	m.files[ref.ID] = memFile{ref, user, data}
	return ref, nil
}

func (m *memFiles) Uploader(_ context.Context, _ uint64, id string) (uint64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[id]
	return f.user, ok, nil
}

func (m *memFiles) Open(_ context.Context, _ uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[id]
	return io.NopCloser(bytes.NewReader(f.data)), f.ref, ok, nil
}

func (m *memFiles) Delete(context.Context, uint64, string) error { return nil }

// memUsage keeps the records a recorder writes.
type memUsage struct {
	mu      sync.Mutex
	records []usage.Record
}

func (m *memUsage) Append(_ context.Context, rs []usage.Record, _ []usage.Capture) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rs...)
	return nil
}

func (m *memUsage) PurgeCaptures(context.Context, time.Time) (int64, error) { return 0, nil }

// standIn answers generateContent from a cassette, recording the requests.
func standIn(t *testing.T, cassette string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	reply, err := os.ReadFile("testdata/" + cassette)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-2.5-flash-image:generateContent") {
			http.Error(w, `{"error":{"code":404,"message":"no such model","status":"NOT_FOUND"}}`, http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func session(t *testing.T, srv *httptest.Server, files *memFiles, rec *usage.Recorder) adapter.Session {
	t.Helper()
	provider := gemini.New(gemini.WithAPIKey("test"), gemini.WithBaseURL(srv.URL))
	deps := images.Deps{Images: usage.ImageProvider(provider, "gemini", rec), Files: files}
	sess, err := images.Impl(func() images.Deps { return deps }).Open(adapter.Context{Context: context.Background()}, adapter.Connection{Type: images.TypeID})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func call(sess adapter.Session, op string, args any) (json.RawMessage, error) {
	raw, _ := json.Marshal(args)
	p := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: 1}
	ctx := adapter.Context{Context: context.Background(), Contract: images.GenerateRef, Operation: op, Args: features.ServiceArgs{Ctx: context.Background(), Principal: p}}
	return sess.Call(ctx, op, raw)
}

func TestGenerate(t *testing.T) {
	srv, got := standIn(t, "generate.json")
	files := &memFiles{files: map[string]memFile{}}
	store := &memUsage{}
	rec := usage.NewRecorder(store, func(context.Context) (aisettings.Models, error) {
		return aisettings.Models{Models: []aisettings.Model{{ID: "img", Provider: "gemini", Model: "gemini-2.5-flash-image", Kind: "image",
			Pricing: aisettings.Pricing{Currency: "USD", InputPerMTok: 0.30, OutputPerMTok: 30}}}}, nil
	}, nil, nil)
	sess := session(t, srv, files, rec)

	out, err := call(sess, "generate", map[string]any{"prompt": "A lighthouse at dusk", "aspectRatio": "16:9", "count": 2, "style": "watercolour"})
	if err != nil {
		t.Fatal(err)
	}
	var r images.GenerateResult
	_ = json.Unmarshal(out, &r)
	if len(r.Images) != 2 || r.Model != "gemini-2.5-flash-image" || r.Images[0].Prompt != "A lighthouse at dusk, in watercolour" ||
		r.Images[0].File.ContentType != "image/png" || r.Images[0].File.Name != "image-1.png" {
		t.Fatalf("result %s", out)
	}
	if f := files.files[r.Images[1].File.ID]; f.user != 7 || len(f.data) == 0 || !bytes.HasPrefix(f.data, []byte("\x89PNG")) {
		t.Errorf("stored %+v", f.ref)
	}
	if len(*got) != 2 {
		t.Fatalf("%d requests", len(*got))
	}
	cfg, _ := json.Marshal((*got)[0]["generationConfig"])
	if !strings.Contains(string(cfg), `"IMAGE"`) || !strings.Contains(string(cfg), `"16:9"`) {
		t.Errorf("generation config %s", cfg)
	}

	// A reference the caller uploaded is sent inline; another's is refused.
	if _, err := call(sess, "generate", map[string]any{"prompt": "The same, at night", "reference": []map[string]string{{"id": r.Images[0].File.ID}}}); err != nil {
		t.Fatal(err)
	}
	parts, _ := json.Marshal((*got)[2]["contents"])
	if !strings.Contains(string(parts), "inlineData") {
		t.Errorf("no inline reference: %s", parts)
	}
	files.files["other"] = memFile{records.FileRef{ID: "other", ContentType: "image/png"}, 8, []byte("x")}
	var ce *connectors.Error
	if _, err := call(sess, "generate", map[string]any{"prompt": "x", "reference": []map[string]string{{"id": "other"}}}); !errors.As(err, &ce) || ce.Code != connectors.CodeNotFound {
		t.Errorf("another's reference: %v", err)
	}
	if _, err := call(sess, "generate", map[string]any{"prompt": " "}); !errors.As(err, &ce) || ce.Code != connectors.CodeInvalid {
		t.Errorf("an empty prompt: %v", err)
	}
	if _, err := call(sess, "generate", map[string]any{"prompt": "x", "count": 9}); !errors.As(err, &ce) || ce.Code != connectors.CodeInvalid {
		t.Errorf("too many: %v", err)
	}

	// Usage: every call recorded as image, priced by its tokens.
	rec.Close()
	if len(store.records) != 2 {
		t.Fatalf("%d usage records", len(store.records))
	}
	first := store.records[0]
	want := aisettings.Pricing{InputPerMTok: 0.30, OutputPerMTok: 30}.CostMicros(aisettings.Tokens{Input: 24, Output: 2580}) // two images
	if first.Feature != "image" || first.UserID != 7 || first.OutputTokens != 2580 || first.CostMicros != want || first.Currency != "USD" {
		t.Errorf("usage %+v, want cost %d", first, want)
	}
}

func TestRefused(t *testing.T) {
	srv, _ := standIn(t, "refused.json")
	sess := session(t, srv, &memFiles{files: map[string]memFile{}}, nil)
	_, err := call(sess, "generate", map[string]any{"prompt": "something unsafe"})
	var ce *connectors.Error
	if !errors.As(err, &ce) || ce.Code != connectors.CodeRefused || ce.Retryable() || !strings.Contains(ce.Message, "safety") {
		t.Fatalf("refusal: %v", err)
	}
}

func TestContractPassesTheChecks(t *testing.T) {
	if ps := connectors.CheckContract(images.Contract()); len(ps) > 0 {
		t.Fatal(ps)
	}
	if ps := connectors.CheckType(images.Type(), func(ref string) (connectors.Contract, bool) {
		return images.Contract(), ref == images.GenerateRef
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}
