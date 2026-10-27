package usage

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the usage resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "usage", Source: "usage", Order: 40}, Title: "Usage and cost", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See usage and cost", Class: authz.ClassRead},
			{Name: "readCapture", Title: "See captured prompts and replies", Class: authz.ClassRead},
			{Name: "viewAll", Title: "See other users' usage", Class: authz.ClassAdmin},
		},
	})
}
