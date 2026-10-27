package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

// ContextProviders lists the chat.contextProvider contributions.
func (s *service) ContextProviders(args conversationservice.ContextProvidersArgs) conversationservice.ContextProvidersResponse {
	if failed := check(args.ServiceArgs, "conversation.read", 0); failed != nil {
		return conversationservice.ContextProvidersResponse{Common: *failed}
	}
	resp := conversationservice.ContextProvidersResponse{Common: features.OK()}
	for _, p := range conversation.ContextProviders.All(s.registry) {
		resp.Providers = append(resp.Providers, conversationservice.ProviderInfo{ID: p.ID, Title: p.Title})
	}
	return resp
}
