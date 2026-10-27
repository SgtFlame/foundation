package authz

import (
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Class is what kind of thing an action does. Built-in roles are defined by
// classes, so a new feature's actions join them with no role changes.
type Class string

const (
	ClassRead     Class = "read"     // see and open it
	ClassWrite    Class = "write"    // change its content
	ClassCreate   Class = "create"   // create children under it
	ClassDelete   Class = "delete"   // delete it
	ClassShare    Class = "share"    // grant others access to it
	ClassModerate Class = "moderate" // approve or reject shares and flagged content
	ClassExecute  Class = "execute"  // run it
	ClassAdmin    Class = "admin"    // change its settings and the grants on it
	// ClassSelf is what every signed-in user may do with no grant: their own
	// account, reading settings, forwarding their browser's logs.
	ClassSelf Class = "self"
)

// Where a permission is held: on the application, or on a tenant (and
// everything under it).
const (
	OnApp    = "app"
	OnTenant = "tenant"
)

// Action is one action of a resource type.
type Action struct {
	Name  string // "write"; the permission is "<type>.<name>"
	Title string
	Class Class
	// On is where a grant must be held for the action to apply: OnApp for
	// tenant.create, OnTenant (the default) for content.
	On string
}

// ResourceType is a kind of resource and its actions, contributed by the
// feature that owns it to the authz.resourceType point (RBAC.md ›
// Resources). The Role Editor builds its permission matrix from these.
type ResourceType struct {
	extensions.Descriptor // ID is the type, e.g. "volume"
	Title                 string
	Parent                string // the type it lives under: "app" or "tenant"
	// Owned types are user-owned: an action on a resource whose path names
	// the principal (".../user:<id>/...") is allowed, the implied Owner.
	Owned   bool
	Actions []Action
}

// ResourceTypes is the authz.resourceType point.
var ResourceTypes = extensions.NewPoint[ResourceType]("authz.resourceType", "Resource types and their actions, for access control and the Role Editor")

// Permission is "<type>.<action>".
func Permission(resourceType, action string) string {
	return resourceType + "." + action
}

// actionOf finds the declared action of a permission.
func actionOf(reg *extensions.Registry, permission string) (ResourceType, Action, bool) {
	typ, name, ok := strings.Cut(permission, ".")
	if !ok {
		return ResourceType{}, Action{}, false
	}
	for _, t := range ResourceTypes.All(reg) {
		if t.ID != typ {
			continue
		}
		for _, a := range t.Actions {
			if a.Name == name {
				if a.On == "" {
					a.On = OnTenant
				}
				return t, a, true
			}
		}
	}
	return ResourceType{}, Action{}, false
}

// Contribute registers authz's own resource types: roles and grants.
func Contribute(reg *extensions.Registry) {
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "role", Source: "authz"}, Title: "Roles", Parent: "tenant",
		Actions: []Action{
			{Name: "read", Title: "See roles", Class: ClassAdmin},
			{Name: "write", Title: "Define custom roles", Class: ClassAdmin},
		},
	})
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "grant", Source: "authz"}, Title: "Grants", Parent: "tenant",
		Actions: []Action{
			{Name: "read", Title: "See who has access", Class: ClassAdmin},
			{Name: "write", Title: "Grant and revoke access", Class: ClassAdmin},
		},
	})
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "secret", Source: "authz"}, Title: "Server secrets", Parent: "app",
		Actions: []Action{
			{Name: "list", Title: "List secrets (never their values)", Class: ClassAdmin, On: OnApp},
			{Name: "write", Title: "Write secrets", Class: ClassAdmin, On: OnApp},
		},
	})
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "extensions", Source: "authz"}, Title: "Extensions", Parent: "app",
		Actions: []Action{{Name: "read", Title: "List extension points", Class: ClassSelf, On: OnApp}},
	})
}
