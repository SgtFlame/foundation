package db

import (
	"github.com/SgtFlame/foundation/backend/features"
	"gorm.io/gorm"
)

// Every new table that belongs to a tenant embeds Tenanted after Model, and
// one that a user owns (a conversation, a Chronicle entry, a draft) embeds
// Owned too, even while there is a single tenant: retrofitting ownership
// later is the expensive path. Services set both from the principal, never
// from a request body, and filter with InTenant.
//
//	type Conversation struct {
//		db.Model
//		db.Tenanted
//		db.Owned
//		Title string
//	}
//	c := Conversation{Tenanted: db.TenantOf(args.Principal), Owned: db.OwnedBy(args.Principal)}
//	gdb.Scopes(db.InTenant(args.Principal.TenantID)).Find(&list)
type Tenanted struct {
	TenantID uint64 `gorm:"index;not null"`
}

// Owned records the user who owns a row; RBAC gives them an implied Owner role on it.
type Owned struct {
	OwnerID uint64 `gorm:"index;not null"`
}

// TenantOf is the principal's tenant, for a new row.
func TenantOf(p features.Principal) Tenanted {
	return Tenanted{TenantID: p.TenantID}
}

// OwnedBy is the principal's user, for a new row.
func OwnedBy(p features.Principal) Owned {
	return Owned{OwnerID: p.UserID}
}

// InTenant is a query scope keeping only the rows of tenant.
func InTenant(tenant uint64) func(*gorm.DB) *gorm.DB {
	return func(q *gorm.DB) *gorm.DB {
		return q.Where("tenant_id = ?", tenant)
	}
}
