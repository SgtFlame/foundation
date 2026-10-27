package cognition

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the cognition resource type (graphs and their
// traces).
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "cognition", Source: "cognition", Order: 80}, Title: "Cognition", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See graphs and their runs", Class: authz.ClassRead},
			{Name: "execute", Title: "Run graphs", Class: authz.ClassExecute},
		},
	})
}
