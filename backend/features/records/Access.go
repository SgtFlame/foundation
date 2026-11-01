package records

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the records resource type: the definitions of
// entity types. Records themselves get the entity type in Forge's part 3g.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "records", Source: "records", Order: 85}, Title: "Data model", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See entity types", Class: authz.ClassRead},
			{Name: "write", Title: "Edit entity type drafts", Class: authz.ClassWrite},
			{Name: "publish", Title: "Publish entity types, so records follow their new version", Class: authz.ClassWrite},
		},
	})
}

// Resource is an entity type's authz resource.
func Resource(p features.Principal, id string) string {
	return authz.Tenant(p) + "/records/entity:" + id
}
