package config

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the config resource type. A section's resource
// is its scope's path plus "config:<section>": "app/config:ai.models",
// "tenant:1/config:chat", "user:2/config:chat". Settings are user-owned at
// the user scope, so everyone writes their own (RBAC question 1).
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "config", Source: "config", Order: 20}, Title: "Settings", Parent: "tenant", Owned: true,
		Actions: []authz.Action{
			{Name: "read", Title: "Read settings", Class: authz.ClassSelf},
			{Name: "write", Title: "Change settings", Class: authz.ClassAdmin},
			{Name: "writeSecrets", Title: "Change secret settings", Class: authz.ClassAdmin},
		},
	})
}
