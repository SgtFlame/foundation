package authz

import (
	"context"
	"encoding/json"
	"slices"

	"gorm.io/gorm"
)

// AppRole is one of an app's roles as its install makes it (forge 05c): a
// custom role of the tenant that only the app changes. Its permissions are
// scoped to the app's own resources ("app:tasks/entity:task/record.read",
// see Role.HoldsOn), so it cannot reach beyond the app (forge 05c
// question 1), and Rules are its row rules by entity type.
type AppRole struct {
	ID          string
	Name        string
	Description string
	Permissions []string
	Rules       map[string]string
}

// SyncAppRoles makes an app's roles in the tenant what roles says, as the
// system (an install or upgrade): new ones are made, changed ones changed,
// and those the app no longer has removed with their grants. It returns
// each role's ID by its id in the manifest.
func (pol *Policy) SyncAppRoles(ctx context.Context, tenant uint64, app string, roles []AppRole) (map[string]uint64, error) {
	ids := map[string]uint64{}
	err := pol.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []CustomRole
		if err := tx.Where("tenant_id = ? AND app_id = ?", tenant, app).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range roles {
			perms := slices.Clone(r.Permissions)
			slices.Sort(perms)
			encoded, _ := json.Marshal(perms)
			rules := ""
			if len(r.Rules) > 0 {
				raw, _ := json.Marshal(r.Rules)
				rules = string(raw)
			}
			i := slices.IndexFunc(rows, func(c CustomRole) bool { return c.AppRole == r.ID })
			if i < 0 {
				row := CustomRole{TenantID: tenant, AppID: app, AppRole: r.ID, Name: r.Name, Description: r.Description, Permissions: string(encoded), Rules: rules, Revision: 1}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				ids[r.ID] = row.ID
				continue
			}
			row := rows[i]
			ids[r.ID] = row.ID
			if row.Name == r.Name && row.Description == r.Description && row.Permissions == string(encoded) && row.Rules == rules {
				continue
			}
			row.Name, row.Description, row.Permissions, row.Rules, row.Revision = r.Name, r.Description, string(encoded), rules, row.Revision+1
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		for _, row := range rows {
			if _, kept := ids[row.AppRole]; !kept {
				if err := pol.removeRole(ctx, tx, row); err != nil {
					return err
				}
			}
		}
		return nil
	})
	pol.Changed()
	return ids, err
}

// RemoveAppRoles removes an app's roles from the tenant with every grant
// of them, as when it is uninstalled.
func (pol *Policy) RemoveAppRoles(ctx context.Context, tenant uint64, app string) error {
	err := pol.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []CustomRole
		if err := tx.Where("tenant_id = ? AND app_id = ?", tenant, app).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := pol.removeRole(ctx, tx, row); err != nil {
				return err
			}
		}
		return nil
	})
	pol.Changed()
	return err
}

// removeRole deletes a custom role and its grants, as the system.
func (pol *Policy) removeRole(ctx context.Context, tx *gorm.DB, row CustomRole) error {
	var grants []RoleGrant
	if err := tx.Where("role_id = ?", row.ID).Find(&grants).Error; err != nil {
		return err
	}
	for _, g := range grants {
		if _, err := pol.Revoke(ctx, tx, g.ID, 0); err != nil {
			return err
		}
	}
	return tx.Delete(&row).Error
}
