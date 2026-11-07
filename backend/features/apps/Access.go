package apps

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the app resource type: application
// definitions, on paths tenant:<id>/app:<appId> (forge 05a question 3).
// Making an app needs app.write on the tenant.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "app", Source: "apps", Order: 92}, Title: "App definitions", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See apps' manifests", Class: authz.ClassRead},
			{Name: "write", Title: "Make apps and edit their drafts", Class: authz.ClassWrite},
			{Name: "publish", Title: "Publish apps as new versions", Class: authz.ClassWrite},
			{Name: "install", Title: "Install, upgrade and uninstall apps", Class: authz.ClassAdmin},
		},
	})
}

// Resource is an app's authz resource; the empty id is the tenant, where
// apps are made.
func Resource(p features.Principal, id string) string {
	if id == "" {
		return authz.Tenant(p)
	}
	return authz.Tenant(p) + "/app:" + id
}
