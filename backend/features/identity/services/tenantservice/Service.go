package tenantservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
)

// Service manages tenants and their members, for administrators
// (the Tenant editor; documentation/MultiTenant.md › Built in slice 10).
type Service interface {
	List(args ListArgs) ListResponse
	Create(args CreateArgs) CreateResponse
	Rename(args RenameArgs) RenameResponse
	// Delete removes a tenant and its memberships, ending its sessions. The
	// default tenant cannot be deleted. The tenant's content stays in the
	// tenant database and its LanceDB directory for now.
	Delete(args DeleteArgs) DeleteResponse
	Members(args MembersArgs) MembersResponse
	AddMember(args AddMemberArgs) AddMemberResponse
	// RemoveMember ends the user's sessions in the tenant.
	RemoveMember(args RemoveMemberArgs) RemoveMemberResponse
}

// Summary is a tenant with its member count.
type Summary struct {
	identity.Tenant
	Members int64
}

type ListArgs struct {
	features.ServiceArgs
}

type ListResponse struct {
	Common  features.ServiceResponse
	Tenants []Summary
}

type CreateArgs struct {
	features.ServiceArgs
	Name string
}

type CreateResponse struct {
	Common features.ServiceResponse
	Tenant identity.Tenant
}

type RenameArgs struct {
	features.ServiceArgs
	TenantID uint64
	Name     string
}

type RenameResponse struct {
	Common features.ServiceResponse
	Tenant identity.Tenant
}

type DeleteArgs struct {
	features.ServiceArgs
	TenantID uint64
}

type DeleteResponse struct {
	Common features.ServiceResponse
}

type MembersArgs struct {
	features.ServiceArgs
	TenantID uint64
}

type MembersResponse struct {
	Common features.ServiceResponse
	Users  []identity.User
	Roles  map[uint64][]string // the role names each member holds on the tenant
}

type AddMemberArgs struct {
	features.ServiceArgs
	TenantID uint64
	UserID   uint64
}

type AddMemberResponse struct {
	Common features.ServiceResponse
}

type RemoveMemberArgs struct {
	features.ServiceArgs
	TenantID uint64
	UserID   uint64
}

type RemoveMemberResponse struct {
	Common features.ServiceResponse
}
