package conversation

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the conversation resource type. Conversations
// are private to their owner, which conversationservice's queries enforce,
// so every member may chat.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "conversation", Source: "conversation", Order: 50}, Title: "Conversations", Parent: "tenant", Owned: true,
		Actions: []authz.Action{
			{Name: "read", Title: "Read your conversations", Class: authz.ClassSelf},
			{Name: "write", Title: "Chat", Class: authz.ClassSelf},
		},
	})
}
