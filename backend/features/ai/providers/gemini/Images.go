package gemini

import (
	"context"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"google.golang.org/genai"
)

// DefaultImageModel is Gemini's native image model (forge 35f-7 question 1).
const DefaultImageModel = "gemini-2.5-flash-image"

// GenerateImages makes images with Gemini's native image generation: one
// generateContent call per image asked for, with image output, the
// references sent as inline parts. A blocked prompt or a finish for safety
// with no image is a refusal, not an error.
func (p *Provider) GenerateImages(ctx context.Context, in providers.ImageInput) (providers.ImageOutput, error) {
	out := providers.ImageOutput{Model: in.Model}
	if out.Model == "" {
		out.Model = DefaultImageModel
	}
	client, err := p.getClient(ctx)
	if err != nil {
		return out, err
	}
	parts := []*genai.Part{{Text: in.Prompt}}
	for _, r := range in.References {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: r.Data, MIMEType: r.ContentType}})
	}
	config := &genai.GenerateContentConfig{ResponseModalities: []string{"TEXT", "IMAGE"}}
	if in.AspectRatio != "" {
		config.ImageConfig = &genai.ImageConfig{AspectRatio: in.AspectRatio}
	}
	count := max(in.Count, 1)
	var texts []string
	for range count {
		resp, err := client.Models.GenerateContent(ctx, out.Model, []*genai.Content{{Role: genai.RoleUser, Parts: parts}}, config)
		if err != nil {
			return out, wrap(err)
		}
		if u := resp.UsageMetadata; u != nil {
			out.Usage.InputTokens += int(u.PromptTokenCount)
			out.Usage.OutputTokens += int(u.CandidatesTokenCount)
		}
		if f := resp.PromptFeedback; f != nil && f.BlockReason != "" {
			out.Refused, out.Reason = true, strings.TrimSpace(string(f.BlockReason)+" "+f.BlockReasonMessage)
			break
		}
		made := false
		for _, c := range resp.Candidates {
			if c.Content != nil {
				for _, part := range c.Content.Parts {
					switch {
					case part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/"):
						out.Images = append(out.Images, providers.Image{Data: part.InlineData.Data, ContentType: part.InlineData.MIMEType})
						made = true
					case part.Text != "" && !part.Thought:
						texts = append(texts, strings.TrimSpace(part.Text))
					}
				}
			}
			if !made && refusedFinish(c.FinishReason) {
				out.Refused, out.Reason = true, string(c.FinishReason)
			}
		}
		if out.Refused && len(out.Images) == 0 {
			break
		}
	}
	if len(out.Images) > 0 {
		out.Refused, out.Reason = false, ""
	}
	out.Text = strings.Join(texts, "\n")
	return out, nil
}

func refusedFinish(r genai.FinishReason) bool {
	switch r {
	case genai.FinishReasonSafety, genai.FinishReasonProhibitedContent, genai.FinishReasonBlocklist, genai.FinishReasonSPII,
		genai.FinishReasonImageSafety, genai.FinishReasonImageProhibitedContent, genai.FinishReasonNoImage:
		return true
	}
	return false
}

var _ providers.Images = (*Provider)(nil)
