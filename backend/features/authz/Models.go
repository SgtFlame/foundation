package authz

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// CustomRole is a role a tenant's administrator defines (RBAC.md › Roles):
// usable only within that tenant, holding only tenant permissions. Built-in
// roles are code (Builtins), not rows.
type CustomRole struct {
	db.Model
	TenantID    uint64 `gorm:"index;not null"`
	Name        string `gorm:"not null"`
	Description string
	Permissions string // JSON array of "<type>.<action>"
	Revision    int
}

// RoleGrant gives a principal a role on a resource and everything under it.
type RoleGrant struct {
	db.Model
	UserID    uint64 `gorm:"index;not null"`
	RoleID    uint64 `gorm:"index;not null"` // a built-in role's reserved ID, or a CustomRole
	Resource  string `gorm:"index;not null"` // "app", "tenant:1", "tenant:1/volume:42"
	GrantedBy uint64 // 0: the system (setup, migration)
	ExpiresAt *time.Time
}

// Audit kinds.
const (
	AuditGrant  = "grant"
	AuditRevoke = "revoke"
	AuditDenied = "denied"
)

// AccessAudit records every grant change and every denied check of an
// action that changes something (RBAC.md › Evaluation), for the audit viewer.
type AccessAudit struct {
	db.Model
	Kind     string `gorm:"index"`
	UserID   uint64 `gorm:"index"` // whose access: the grantee, or the denied principal
	ActorID  uint64 // who granted or revoked
	TenantID uint64
	Action   string // the denied action, or the role's name
	Resource string
	Reason   string
}

// Models are authz's tables, for the application database: grants span
// tenants.
func Models() []any {
	return []any{&CustomRole{}, &RoleGrant{}, &AccessAudit{}}
}
