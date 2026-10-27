package embeddto

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type EmbedResponse struct {
	rest.Status
	Vectors     [][]float32 `json:"vectors,omitempty"`
	Model       string      `json:"model,omitempty"`
	Dimensions  int         `json:"dimensions,omitempty"`
	InputTokens int         `json:"inputTokens,omitempty"`
}

// EmbedResponseFrom converts the service's EmbedResponse; its code is carried
// by the HTTP status rather than the body.
func EmbedResponseFrom(resp embedservice.EmbedResponse) EmbedResponse {
	return EmbedResponse{
		Status:      rest.StatusFrom(resp.Common),
		Vectors:     resp.Vectors,
		Model:       resp.Model,
		Dimensions:  resp.Dimensions,
		InputTokens: resp.Usage.InputTokens,
	}
}
