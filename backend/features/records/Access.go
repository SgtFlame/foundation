package records

import (
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the records resource type: the definitions of
// entity types. Records themselves get the entity type in Forge's part 3g.
func ContributeAccess(reg *extensions.Registry) {
	ContributeRecordAccess(reg)
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "records", Source: "records", Order: 85}, Title: "Data model", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See entity types", Class: authz.ClassRead},
			{Name: "write", Title: "Edit entity type drafts", Class: authz.ClassWrite},
			{Name: "publish", Title: "Publish entity types, so records follow their new version", Class: authz.ClassWrite},
		},
	})
}

// ContributeRecordAccess declares the record resource type: the records
// of an entity type, on the entity type's path (Resource). Behind the app
// fence (forge 05c question 5), an installed app's records are reached
// only through its roles and the tenant's Owner and Administrator.
func ContributeRecordAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "record", Source: "records", Order: 86}, Title: "Records", Parent: "tenant", Fence: "app",
		Actions: []authz.Action{
			{Name: "read", Title: "See records", Class: authz.ClassRead},
			{Name: "create", Title: "Create records", Class: authz.ClassCreate},
			{Name: "update", Title: "Change records", Class: authz.ClassWrite},
			{Name: "delete", Title: "Delete records", Class: authz.ClassDelete},
		},
	})
}

// Resource is an entity type's authz resource: tenant:<id>/records/
// entity:<id>, or, for a type owned by an installed app,
// tenant:<id>/app:<appId>/entity:<id> (forge 05c question 3).
func Resource(p features.Principal, id string) string {
	if place := placement.Load(); place != nil {
		if app := (*place)(p.TenantID, id); app != "" {
			return authz.Tenant(p) + "/app:" + app + "/entity:" + id
		}
	}
	return authz.Tenant(p) + "/records/entity:" + id
}

// Placement finds the installed app whose own an entity type is in a
// tenant, empty for none.
type Placement func(tenant uint64, entity string) string

var placement atomic.Pointer[Placement]

// SetPlacement sets where Resource finds an entity type's app (the apps'
// installs, wired by serve.go); nil puts every type at the tenant's path.
func SetPlacement(fn Placement) {
	if fn == nil {
		placement.Store(nil)
		return
	}
	placement.Store(&fn)
}
