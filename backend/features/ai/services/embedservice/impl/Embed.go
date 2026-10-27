package impl

import (
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

// Embed embeds args.Texts, splitting them into as many provider calls as the
// batch size needs.
func (s *service) Embed(args embedservice.EmbedArgs) embedservice.EmbedResponse {
	if s.provider == nil {
		return embedFail(args, providers.ErrNotConfigured.Code, providers.Wrap(providers.ErrNotConfigured))
	}
	switch args.Purpose {
	case "":
		args.Purpose = embedservice.PurposeDocument
	case embedservice.PurposeDocument, embedservice.PurposeQuery:
	default:
		return embedFail(args, http.StatusBadRequest, log.Code("unknown_purpose").Errorf("unknown purpose %s", args.Purpose))
	}
	if len(args.Texts) == 0 {
		return embedFail(args, http.StatusBadRequest, log.Code("no_texts").New("no texts to embed"))
	}
	for _, text := range args.Texts {
		if strings.TrimSpace(text) == "" {
			return embedFail(args, http.StatusBadRequest, log.Code("empty_text").New("cannot embed an empty text"))
		}
	}
	ctx := args.CallContext()

	info := s.provider.EmbedModel()
	model := args.Model
	if model == "" {
		model = info.Model
	}
	resp := embedservice.EmbedResponse{Model: model, Vectors: make([][]float32, 0, len(args.Texts))}
	n := s.batch()
	for start := 0; start < len(args.Texts); start += n {
		texts := args.Texts[start:min(start+n, len(args.Texts))]
		out, err := s.provider.Embed(ctx, embedservice.EmbedInput{Model: model, Texts: texts, Purpose: args.Purpose, Title: args.Title})
		if err != nil {
			return embedFail(args, providers.Code(err), providers.Wrap(err))
		}
		if len(out.Vectors) != len(texts) {
			return embedFail(args, http.StatusBadGateway, log.Code("provider_failed").Public("the AI provider returned the wrong number of embeddings").Errorf("provider returned %d embeddings for %d texts", len(out.Vectors), len(texts)))
		}
		resp.Vectors = append(resp.Vectors, out.Vectors...)
		resp.Usage.InputTokens += out.Usage.InputTokens
	}
	resp.Dimensions = len(resp.Vectors[0])
	resp.Common = features.OK()
	return resp
}

func embedFail(args embedservice.EmbedArgs, code uint16, err error) embedservice.EmbedResponse {
	return embedservice.EmbedResponse{Common: *args.Fail(code, err)}
}
