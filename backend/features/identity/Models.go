// Package identity is who uses Foundation: tenants, users and their
// memberships, local sign-in with argon2id passwords, and sessions as a
// short-lived JWT plus a rotating refresh token
// (documentation/MultiTenant.md › Built in slice 10). Its tables live
// in the application database, which spans tenants.
package identity

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// Tenant is an organisation or workspace. The first one, made by setup,
// has the reserved ID features.LocalTenantID, so rows written in
// single-user mode stay its own.
type Tenant struct {
	db.Model
	Name string `gorm:"not null"`
}

// User is a person who signs in. The first one, made by setup, has the
// reserved ID features.LocalUserID and holds Super Administrator.
type User struct {
	db.Model
	Username     string `gorm:"uniqueIndex;not null"` // lower case
	DisplayName  string
	Email        string
	PasswordHash string `gorm:"not null"` // argon2id, in PHC form
	// SuperAdmin tells whether the user holds Super Administrator on the
	// application: not a column but read from the grants (MarkSuperAdmins),
	// which replaced slice 10's administrator flag (RBAC question 7).
	SuperAdmin bool `gorm:"-"`
	Disabled   bool
	// LastTenantID is the tenant the user last worked in, where sign-in
	// starts.
	LastTenantID uint64
	// PolicyVersion goes up whenever what the user may do changes; tokens
	// carry it (MultiTenant.md › Identity).
	PolicyVersion int
}

// Membership puts a user in a tenant. What they may do there comes from
// their role grants (features/authz).
type Membership struct {
	db.Model
	TenantID uint64 `gorm:"uniqueIndex:membership;not null"`
	UserID   uint64 `gorm:"uniqueIndex:membership;index;not null"`
}

// RefreshToken is a session's refresh token, stored only as its SHA-256.
// Each use replaces it with a new one of the same Family; using a replaced
// one again (after a short grace) revokes the whole family.
type RefreshToken struct {
	db.Model
	Hash       string `gorm:"uniqueIndex;not null"`
	Family     uint64 `gorm:"index;not null"`
	UserID     uint64 `gorm:"index;not null"`
	TenantID   uint64 `gorm:"not null"`
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedAt *time.Time // when it was rotated, for the grace period
}

// SigningKey is the key tokens are signed with, sealed by config.Cipher,
// whose key is Vault's server/db-key when the server uses Vault
// (Secrets.md). There is one row.
type SigningKey struct {
	db.Model
	Sealed string `gorm:"not null"`
}

// Models are identity's tables, for the application database.
func Models() []any {
	return []any{&Tenant{}, &User{}, &Membership{}, &RefreshToken{}, &SigningKey{}}
}
