package ui

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the ui resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "ui", Source: "ui", Order: 90}, Title: "Applications", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See the installed applications and the component catalog", Class: authz.ClassRead},
			{Name: "design", Title: "Change applications' pages, such as adding a button", Class: authz.ClassWrite},
		},
	})
}

// ContributePrefabAccess declares the prefabs resource type: prefab
// definitions, on paths tenant:<id>/ui/prefab:<prefabId> (forge 04a
// question 1). App-owned prefabs follow the app's roles once apps exist.
func ContributePrefabAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "prefabs", Source: "ui", Order: 91}, Title: "Prefabs", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See prefabs and their versions", Class: authz.ClassRead},
			{Name: "write", Title: "Edit prefab drafts", Class: authz.ClassWrite},
			{Name: "publish", Title: "Publish prefabs, so their instances follow the new version", Class: authz.ClassWrite},
		},
	})
}

// PrefabResource is a prefab's authz resource; the empty id is the
// tenant's prefabs.
func PrefabResource(p features.Principal, id string) string {
	if id == "" {
		return authz.Tenant(p) + "/ui"
	}
	return authz.Tenant(p) + "/ui/prefab:" + id
}
