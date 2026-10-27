package identity

import (
	"context"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

var log = logging.In("identity")

// DefaultMemberRole is the role a new member gets on their tenant, and
// CreatorRole the role of whoever makes a tenant (RBAC question 7).
const (
	DefaultMemberRole = authz.RoleEditor
	CreatorRole       = authz.RoleOwner
)

// Grant gives a role inside tx through the installed policy; without one
// (tests of identity alone) it does nothing. Call authz.Installed().Changed()
// once tx commits, through Changed.
func Grant(ctx context.Context, tx *gorm.DB, args authz.GrantArgs) error {
	pol := authz.Installed()
	if pol == nil {
		return nil
	}
	_, err := pol.Grant(ctx, tx, args)
	return err
}

// RevokeUnder removes user's grants on resource and below inside tx (user
// 0: everyone's).
func RevokeUnder(ctx context.Context, tx *gorm.DB, user uint64, resource string, by uint64) error {
	pol := authz.Installed()
	if pol == nil {
		return nil
	}
	return pol.RevokeUnder(ctx, tx, user, resource, by)
}

// Changed drops the policy's caches after a transaction that changed grants.
func Changed() {
	if pol := authz.Installed(); pol != nil {
		pol.Changed()
	}
}

// SuperAdmins is the users holding Super Administrator, read inside tx.
func SuperAdmins(tx *gorm.DB) ([]uint64, error) {
	pol := authz.Installed()
	if pol == nil {
		return nil, nil
	}
	return pol.SuperAdmins(tx)
}

// MarkSuperAdmins sets SuperAdmin on each user who holds it.
func MarkSuperAdmins(tx *gorm.DB, users []User) error {
	ids, err := SuperAdmins(tx)
	if err != nil {
		return err
	}
	for i := range users {
		users[i].SuperAdmin = slices.Contains(ids, users[i].ID)
	}
	return nil
}

// RolesOn is the names of the roles each user holds on resource exactly.
func RolesOn(resource string, users []uint64) (map[uint64][]string, error) {
	out := map[uint64][]string{}
	pol := authz.Installed()
	if pol == nil {
		return out, nil
	}
	grants, err := pol.GrantsOn(nil, resource, false)
	if err != nil {
		return nil, err
	}
	roles, err := pol.Roles()
	if err != nil {
		return nil, err
	}
	for _, g := range grants {
		if slices.Contains(users, g.UserID) {
			out[g.UserID] = append(out[g.UserID], roles[g.RoleID].Name)
		}
	}
	return out, nil
}

// MigrateRoles replaces slice 10's administrator flag with grants, once
// (RBAC question 7): every user with the flag gets Super Administrator on
// the application, every member Editor on their tenant (the first user
// Owner on the default tenant), and the column is dropped. While no user
// exists the implicit single-user principal holds Super Administrator and
// Owner of the default tenant, so setup's first user starts with them.
func MigrateRoles(ctx context.Context, gdb *gorm.DB, pol *authz.Policy) error {
	err := gdb.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasColumn(&User{}, "admin") {
			var admins []uint64
			if err := tx.Table("users").Where("admin").Pluck("id", &admins).Error; err != nil {
				return err
			}
			for _, id := range admins {
				if _, err := pol.Grant(ctx, tx, authz.GrantArgs{UserID: id, RoleID: authz.RoleSuperAdmin, Resource: authz.OnApp}); err != nil {
					return err
				}
			}
			var members []Membership
			if err := tx.Find(&members).Error; err != nil {
				return err
			}
			for _, m := range members {
				role := DefaultMemberRole
				if m.UserID == features.LocalUserID && m.TenantID == features.LocalTenantID {
					role = CreatorRole
				}
				if _, err := pol.Grant(ctx, tx, authz.GrantArgs{UserID: m.UserID, RoleID: role, Resource: authz.Resource("tenant", m.TenantID)}); err != nil {
					return err
				}
			}
			if err := tx.Exec("ALTER TABLE users DROP COLUMN admin").Error; err != nil {
				return err
			}
			if len(admins)+len(members) > 0 {
				log.Info(ctx, "replaced the administrator flag with roles: %d Super Administrators, %d memberships", len(admins), len(members))
			}
		}
		var users int64
		if err := tx.Model(&User{}).Count(&users).Error; err != nil {
			return err
		}
		if users == 0 {
			return Bootstrap(ctx, tx, pol)
		}
		return nil
	})
	pol.Changed()
	return err
}

// Bootstrap grants the reserved first user Super Administrator and Owner of
// the default tenant: the implicit user of single-user mode, and the user
// setup makes.
func Bootstrap(ctx context.Context, tx *gorm.DB, pol *authz.Policy) error {
	if _, err := pol.Grant(ctx, tx, authz.GrantArgs{UserID: features.LocalUserID, RoleID: authz.RoleSuperAdmin, Resource: authz.OnApp}); err != nil {
		return err
	}
	_, err := pol.Grant(ctx, tx, authz.GrantArgs{UserID: features.LocalUserID, RoleID: CreatorRole, Resource: authz.Resource("tenant", features.LocalTenantID)})
	return err
}
