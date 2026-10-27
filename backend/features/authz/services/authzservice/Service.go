package authzservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
)

// Service administers access (documentation/plans/RBAC.md › Services):
// the resource types, roles and grants the Role Editor and the Share
// dialog show, and what the principal may do, for the browser's useCan.
// Custom roles belong to the principal's tenant.
type Service interface {
	// Me is the principal's effective permissions.
	Me(args MeArgs) MeResponse
	ResourceTypes(args ResourceTypesArgs) ResourceTypesResponse
	// Roles is the built-in roles and the tenant's custom roles.
	Roles(args RolesArgs) RolesResponse
	// SaveRole makes or changes a custom role of the tenant; 409 when the
	// revision is stale. Built-in roles cannot change.
	SaveRole(args SaveRoleArgs) SaveRoleResponse
	// DeleteRole removes a custom role and its grants.
	DeleteRole(args DeleteRoleArgs) DeleteRoleResponse
	// Grants lists the grants on a resource (and, with Under, below it), or
	// of one role in the tenant.
	Grants(args GrantsArgs) GrantsResponse
	// Grant gives a member a role on a resource. The granter needs
	// grant.write there and every permission of the role.
	Grant(args GrantArgs) GrantResponse
	// Revoke removes a grant, with the same rule as Grant.
	Revoke(args RevokeArgs) RevokeResponse
	// Explain decides whether a user may do an action on a resource, and
	// why, without auditing a denial: the Access checker.
	Explain(args ExplainArgs) ExplainResponse
}

type MeArgs struct {
	features.ServiceArgs
}

type MeResponse struct {
	Common    features.ServiceResponse
	Effective authz.Effective
}

type ResourceTypesArgs struct {
	features.ServiceArgs
}

type ResourceTypesResponse struct {
	Common features.ServiceResponse
	Types  []authz.ResourceType
}

type RolesArgs struct {
	features.ServiceArgs
}

type RolesResponse struct {
	Common features.ServiceResponse
	Roles  []authz.Role
}

// SaveRoleArgs saves a custom role; RoleID 0 makes one.
type SaveRoleArgs struct {
	features.ServiceArgs
	RoleID      uint64
	Name        string
	Description string
	Permissions []string
	Revision    int
}

type SaveRoleResponse struct {
	Common features.ServiceResponse
	Role   authz.Role
}

type DeleteRoleArgs struct {
	features.ServiceArgs
	RoleID uint64
}

type DeleteRoleResponse struct {
	Common features.ServiceResponse
}

// GrantsArgs names a Resource (with Under, everything below it too) or a
// RoleID, whose grants within the tenant are listed.
type GrantsArgs struct {
	features.ServiceArgs
	Resource string
	Under    bool
	RoleID   uint64
}

type GrantsResponse struct {
	Common features.ServiceResponse
	Grants []authz.RoleGrant
}

type GrantArgs struct {
	features.ServiceArgs
	UserID    uint64
	RoleID    uint64
	Resource  string
	ExpiresAt *time.Time
}

type GrantResponse struct {
	Common features.ServiceResponse
	Grant  authz.RoleGrant
}

type RevokeArgs struct {
	features.ServiceArgs
	GrantID uint64
}

type RevokeResponse struct {
	Common features.ServiceResponse
}

// ExplainArgs asks about UserID (0: the principal) working in the
// principal's tenant.
type ExplainArgs struct {
	features.ServiceArgs
	UserID   uint64
	Action   string
	Resource string
}

type ExplainResponse struct {
	Common   features.ServiceResponse
	Decision authz.Decision
}
