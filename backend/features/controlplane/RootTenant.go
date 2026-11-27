package controlplane

import (
	"context"
	"errors"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"gorm.io/gorm"
)

// RootTenantName is the root tenant's name when it is made.
const RootTenantName = "Infrastructure"

// EnsureRootTenant makes the root tenant (features.RootTenantID) in the
// application database if it does not exist, with owner (when not 0) its
// member and Owner (part question 1: whoever sets up the control plane, no
// one else, not even a Super Administrator). It tells whether it made it;
// a second call does nothing.
func EnsureRootTenant(ctx context.Context, gdb *gorm.DB, owner uint64) (bool, error) {
	made := false
	err := gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t identity.Tenant
		err := tx.First(&t, features.RootTenantID).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if owner != 0 {
			var u identity.User
			if err := tx.First(&u, owner).Error; err != nil {
				return log.Code("user_not_found").Wrapf(err, "user %d", owner)
			}
		}
		if err := tx.Create(&identity.Tenant{Model: db.Model{ID: features.RootTenantID}, Name: RootTenantName}).Error; err != nil {
			return err
		}
		made = true
		if owner == 0 {
			return nil
		}
		return addOwner(ctx, tx, owner)
	})
	identity.Changed()
	if err != nil {
		return false, log.Wrap(err)
	}
	if made {
		log.With("owner", owner).Info(ctx, "made the root tenant %d", features.RootTenantID)
	}
	return made, nil
}

// AddRootOwner makes a user a member and Owner of the root tenant (`cli
// controlplane init --user`), for a root tenant made without one.
func AddRootOwner(ctx context.Context, gdb *gorm.DB, user uint64) error {
	err := gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u identity.User
		if err := tx.First(&u, user).Error; err != nil {
			return log.Code("user_not_found").Wrapf(err, "user %d", user)
		}
		return addOwner(ctx, tx, user)
	})
	identity.Changed()
	return log.Wrap(err)
}

func addOwner(ctx context.Context, tx *gorm.DB, user uint64) error {
	var n int64
	if err := tx.Model(&identity.Membership{}).Where("tenant_id = ? AND user_id = ?", features.RootTenantID, user).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		if err := tx.Create(&identity.Membership{TenantID: features.RootTenantID, UserID: user}).Error; err != nil {
			return err
		}
	}
	if err := identity.Grant(ctx, tx, authz.GrantArgs{UserID: user, RoleID: identity.CreatorRole, Resource: authz.Resource("tenant", features.RootTenantID)}); err != nil {
		return err
	}
	return tx.Model(&identity.User{}).Where("id = ?", user).UpdateColumn("policy_version", gorm.Expr("policy_version + 1")).Error
}
