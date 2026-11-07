package authz

import (
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Built-in roles have reserved IDs, far below any TSID, so grants can name
// them like custom roles.
const (
	RoleSuperAdmin  uint64 = 1
	RoleTenantAdmin uint64 = 2
	RoleAppAdmin    uint64 = 3
	RoleGlobalMod   uint64 = 4
	RoleOwner       uint64 = 5
	RoleAdmin       uint64 = 6
	RoleModerator   uint64 = 7
	RoleEditor      uint64 = 8
	RoleViewer      uint64 = 9
)

// Role is a named set of permissions, granted at a scope: OnApp roles are
// granted on the application, OnTenant roles on a tenant or below.
type Role struct {
	ID          uint64
	Name        string
	Description string
	Scope       string // OnApp or OnTenant
	TenantID    uint64 // a custom role's tenant
	BuiltIn     bool
	Permissions []string // "*" is every permission
	Revision    int
	// App is the app whose role this is (forge 05c), empty for every other;
	// AppRole is its id in the app's manifest.
	App     string
	AppRole string
	// Rules are an app role's row rules by key (an entity type's id),
	// expressions the owning feature runs (records); see RowRules.
	Rules map[string]string
}

// Holds tells whether the role contains permission.
func (r Role) Holds(permission string) bool {
	return slices.Contains(r.Permissions, "*") || slices.Contains(r.Permissions, permission)
}

// HoldsOn tells whether the role holds action on the resource at path: a
// plain permission anywhere it is granted, and a scoped one
// ("app:tasks/entity:task/record.read", an app's role) only on its scope,
// a path below the tenant, and under it.
func (r Role) HoldsOn(action string, path []string) bool {
	if r.Holds(action) {
		return true
	}
	if len(path) > 0 && strings.HasPrefix(path[0], "tenant:") {
		path = path[1:]
	}
	for _, p := range r.Permissions {
		scope, perm, ok := ScopedPermission(p)
		if ok && perm == action && Covers(scope, path) {
			return true
		}
	}
	return false
}

// ScopedPermission splits an app role's permission into its scope below
// the tenant and the permission: "app:tasks/entity:task/record.read" is
// [app:tasks entity:task] and record.read.
func ScopedPermission(p string) (scope []string, permission string, ok bool) {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return nil, p, false
	}
	return strings.Split(p[:i], "/"), p[i+1:], true
}

// builtin describes a built-in role by the actions it takes.
type builtin struct {
	Role
	takes func(t ResourceType, a Action) bool
}

func classIn(classes ...Class) func(ResourceType, Action) bool {
	return func(_ ResourceType, a Action) bool {
		return a.On == OnTenant && slices.Contains(classes, a.Class)
	}
}

var builtins = []builtin{
	{Role{ID: RoleSuperAdmin, Name: "Super Administrator", Scope: OnApp, Description: "Everything, everywhere"},
		nil},
	{Role{ID: RoleTenantAdmin, Name: "Tenant Administrator", Scope: OnApp, Description: "Create, delete and administer any tenant and its members; no access to tenant content unless also granted"},
		func(t ResourceType, _ Action) bool { return t.ID == "tenant" || t.ID == "user" }},
	{Role{ID: RoleAppAdmin, Name: "Application Administrator", Scope: OnApp, Description: "Maintain the server's secrets, write-only"},
		func(t ResourceType, _ Action) bool { return t.ID == "secret" }},
	{Role{ID: RoleGlobalMod, Name: "Global Moderator", Scope: OnApp, Description: "Moderate content shared to the global tenant"},
		func(_ ResourceType, a Action) bool { return a.On == OnApp && a.Class == ClassModerate }},
	{Role{ID: RoleOwner, Name: "Owner", Scope: OnTenant, Description: "Everything in the tenant, including deleting it"},
		func(_ ResourceType, a Action) bool { return a.On == OnTenant }},
	{Role{ID: RoleAdmin, Name: "Administrator", Scope: OnTenant, Description: "Everything in the tenant but deleting: its settings, members, roles and sharing, and its content"},
		classIn(ClassRead, ClassWrite, ClassCreate, ClassExecute, ClassShare, ClassModerate, ClassAdmin)},
	{Role{ID: RoleModerator, Name: "Moderator", Scope: OnTenant, Description: "Moderate shares out of the tenant and flagged content"},
		classIn(ClassRead, ClassModerate)},
	{Role{ID: RoleEditor, Name: "Editor", Scope: OnTenant, Description: "Read, write, create and run"},
		classIn(ClassRead, ClassWrite, ClassCreate, ClassExecute)},
	{Role{ID: RoleViewer, Name: "Viewer", Scope: OnTenant, Description: "Read"},
		classIn(ClassRead)},
}

// Builtins are the built-in roles, their permissions taken from the
// registry's resource types.
func Builtins(reg *extensions.Registry) []Role {
	out := make([]Role, 0, len(builtins))
	for _, b := range builtins {
		r := b.Role
		r.BuiltIn = true
		if b.takes == nil {
			r.Permissions = []string{"*"}
		} else {
			r.Permissions = []string{}
			for _, t := range ResourceTypes.All(reg) {
				for _, a := range t.Actions {
					if a.On == "" {
						a.On = OnTenant
					}
					if a.Class != ClassSelf && b.takes(t, a) {
						r.Permissions = append(r.Permissions, Permission(t.ID, a.Name))
					}
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// IsBuiltin tells whether id is a built-in role's.
func IsBuiltin(id uint64) bool {
	return id >= RoleSuperAdmin && id <= RoleViewer
}
