package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
)

// Held is one grant of a principal, with its role's permissions.
type Held struct {
	Grant       RoleGrant
	Role        Role
	Permissions []string // "*" expanded to every declared permission
}

// Effective is what a principal may do, for the browser to hide or disable
// what it may not (useCan): their grants with their roles' permissions,
// the self actions and the user-owned types. The server check stays
// authoritative.
type Effective struct {
	All         bool // a service principal, which may do anything
	Grants      []Held
	Self        []string          // permissions every signed-in user has
	OwnedTypes  []string          // resource types the principal owns under "user:<id>"
	Fences      map[string]string // resource type → the kind of segment it is fenced by (ResourceType.Fence)
	Permissions []string          // every declared permission, for expanding "*"
}

// EffectiveFor is p's effective permissions.
func (pol *Policy) EffectiveFor(p features.Principal) (Effective, error) {
	var e Effective
	for _, t := range ResourceTypes.All(pol.registry) {
		if t.Owned {
			e.OwnedTypes = append(e.OwnedTypes, t.ID)
		}
		if t.Fence != "" {
			if e.Fences == nil {
				e.Fences = map[string]string{}
			}
			e.Fences[t.ID] = t.Fence
		}
		for _, a := range t.Actions {
			perm := Permission(t.ID, a.Name)
			e.Permissions = append(e.Permissions, perm)
			if a.Class == ClassSelf {
				e.Self = append(e.Self, perm)
			}
		}
	}
	switch p.Kind {
	case features.PrincipalService:
		e.All = true
		return e, nil
	case features.PrincipalAnonymous:
		return Effective{}, nil
	}
	roles, err := pol.Roles()
	if err != nil {
		return e, err
	}
	var grants []RoleGrant
	if p.Kind == features.PrincipalApp {
		grants = AppGrants(roles, p.TenantID, p.App)
	} else if grants, err = pol.GrantsOf(p.UserID); err != nil {
		return e, err
	}
	for _, g := range grants {
		role, ok := roles[g.RoleID]
		if !ok {
			continue
		}
		perms := role.Permissions
		if role.Holds("*") {
			perms = e.Permissions
		}
		e.Grants = append(e.Grants, Held{Grant: g, Role: role, Permissions: perms})
	}
	return e, nil
}

// MayGrant decides whether p may grant role on resource: p needs
// grant.write there and must hold every permission of the role there, so
// no one gives away more than they have.
func (pol *Policy) MayGrant(ctx context.Context, p features.Principal, role Role, resource string) Decision {
	if d := pol.Explain(ctx, p, "grant.write", resource); !d.Allowed {
		return d
	}
	perms := role.Permissions
	if role.Holds("*") {
		perms = nil
		for _, t := range ResourceTypes.All(pol.registry) {
			for _, a := range t.Actions {
				perms = append(perms, Permission(t.ID, a.Name))
			}
		}
	}
	for _, perm := range perms {
		on := resource
		// An app role's permission is held on its scope in the tenant
		if scope, plain, ok := ScopedPermission(perm); ok {
			if path := Split(resource); len(path) > 0 {
				on = path[0] + "/" + strings.Join(scope, "/")
			}
			perm = plain
		}
		_, act, ok := actionOf(pol.registry, perm)
		if !ok || act.Class == ClassSelf {
			continue
		}
		if d := pol.Explain(ctx, p, perm, on); !d.Allowed {
			return Decision{Reason: fmt.Sprintf("you do not hold %s on %s yourself", perm, resource)}
		}
	}
	return Decision{Allowed: true, Reason: "you hold every permission of the role"}
}
