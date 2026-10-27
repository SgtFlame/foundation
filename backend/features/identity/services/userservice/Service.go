package userservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
)

// Service manages users, for administrators (the User editor;
// documentation/MultiTenant.md › Built in slice 10). Users are
// disabled, never deleted, so what they own keeps an owner.
type Service interface {
	List(args ListArgs) ListResponse
	Create(args CreateArgs) CreateResponse
	// Update changes what is set. Granting or revoking Super Administrator,
	// disabling or setting a password ends the user's sessions. A Super
	// Administrator cannot demote or disable themselves, and one enabled
	// Super Administrator always remains.
	Update(args UpdateArgs) UpdateResponse
}

// Summary is a user with the tenants they belong to.
type Summary struct {
	identity.User
	TenantIDs []uint64
}

type ListArgs struct {
	features.ServiceArgs
}

type ListResponse struct {
	Common features.ServiceResponse
	Users  []Summary
}

// CreateArgs makes a user in TenantIDs (at least one).
type CreateArgs struct {
	features.ServiceArgs
	Username    string
	DisplayName string
	Email       string
	Password    string
	SuperAdmin  bool     // grant Super Administrator on the application
	TenantIDs   []uint64 // each membership gets identity.DefaultMemberRole
}

type CreateResponse struct {
	Common features.ServiceResponse
	User   Summary
}

type UpdateArgs struct {
	features.ServiceArgs
	UserID      uint64
	DisplayName *string
	Email       *string
	SuperAdmin  *bool // grant or revoke Super Administrator
	Disabled    *bool
	Password    string // a new password, set by the administrator
}

type UpdateResponse struct {
	Common features.ServiceResponse
	User   Summary
}
