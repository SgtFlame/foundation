package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ContextProvidersResponse struct {
	rest.Status
	Providers []ProviderInfo `json:"providers,omitempty"`
}

type ProviderInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ContextProvidersResponseFrom converts the service's response; its code is
// carried by the HTTP status rather than the body.
func ContextProvidersResponseFrom(resp conversationservice.ContextProvidersResponse) ContextProvidersResponse {
	out := ContextProvidersResponse{Status: rest.StatusFrom(resp.Common)}
	for _, p := range resp.Providers {
		out.Providers = append(out.Providers, ProviderInfo{ID: p.ID, Title: p.Title})
	}
	return out
}
