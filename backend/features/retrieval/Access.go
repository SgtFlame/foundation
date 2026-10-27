package retrieval

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the retrieval resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "retrieval", Source: "retrieval", Order: 70}, Title: "Retrieval", Parent: "tenant",
		Actions: []authz.Action{{Name: "read", Title: "Search the tenant's sources", Class: authz.ClassRead}},
	})
}
