package retrieval

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the retrieval resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "retrieval", Source: "retrieval", Order: 70}, Title: "Retrieval", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "Search the tenant's sources", Class: authz.ClassRead, Agents: true},
			// forge 15b: a reviewer's outcome votes on the segments a draft cited, through an app's handler
			{Name: "vote", Title: "Vote on the segments a reply cited", Class: authz.ClassWrite, Apps: true},
		},
	})
}
