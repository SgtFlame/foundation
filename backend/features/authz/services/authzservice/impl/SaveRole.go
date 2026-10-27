package impl

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"gorm.io/gorm"
)

var errStale = errors.New("the role has changed since it was read")

// SaveRole makes or changes one of the tenant's custom roles. It may hold
// only tenant permissions (RBAC.md › Roles), and only ones the principal
// holds on the tenant themselves.
func (s *service) SaveRole(args authzservice.SaveRoleArgs) authzservice.SaveRoleResponse {
	res := authz.Tenant(args.Principal)
	if f := check(args.ServiceArgs, "role.write", res); f != nil {
		return authzservice.SaveRoleResponse{Common: *f}
	}
	if authz.IsBuiltin(args.RoleID) {
		return authzservice.SaveRoleResponse{Common: *args.Fail(http.StatusConflict,
			log.Code("builtin_role").Public("Built-in roles cannot be changed; duplicate one instead").New("built-in roles cannot be changed"))}
	}
	name := strings.TrimSpace(args.Name)
	if name == "" || len(name) > 64 {
		return authzservice.SaveRoleResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("invalid_role").Public("A role needs a name of at most 64 characters").New("bad role name"))}
	}
	perms := []string{}
	for _, p := range args.Permissions {
		if slices.Contains(perms, p) {
			continue
		}
		if !s.tenantPermission(p) {
			return authzservice.SaveRoleResponse{Common: *args.Fail(http.StatusBadRequest,
				log.Code("invalid_role").With("permission", p).Public(p+" is not a tenant permission").New("not a tenant permission"))}
		}
		if !authz.Check(args.Context(), args.Principal, p, res).Allowed {
			return authzservice.SaveRoleResponse{Common: *args.Fail(authz.Status,
				log.Code("access_denied").With("permission", p).Public("You can only give a role permissions you hold: "+p).New("role escalation refused"))}
		}
		perms = append(perms, p)
	}
	slices.Sort(perms)
	encoded, _ := json.Marshal(perms)

	var row authz.CustomRole
	err := s.pol.DB().WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if args.RoleID == 0 {
			row = authz.CustomRole{TenantID: args.Principal.TenantID, Name: name, Description: args.Description, Permissions: string(encoded), Revision: 1}
			return tx.Create(&row).Error
		}
		if err := tx.Where("tenant_id = ?", args.Principal.TenantID).First(&row, args.RoleID).Error; err != nil {
			return err
		}
		if row.Revision != args.Revision {
			return errStale
		}
		row.Name, row.Description, row.Permissions, row.Revision = name, args.Description, string(encoded), row.Revision+1
		return tx.Save(&row).Error
	})
	s.pol.Changed()
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return authzservice.SaveRoleResponse{Common: *args.Fail(http.StatusNotFound, log.Code("role_not_found").With("roleId", args.RoleID).New("there is no such role"))}
	case errors.Is(err, errStale):
		return authzservice.SaveRoleResponse{Common: *args.Fail(http.StatusConflict, log.Code("role_conflict").Public("Someone changed this role; reload it").Wrap(err))}
	case err != nil:
		return authzservice.SaveRoleResponse{Common: *internal(args.ServiceArgs, err)}
	}
	return authzservice.SaveRoleResponse{Common: features.OK(), Role: authz.RoleFrom(row)}
}

// tenantPermission tells whether p is a declared permission held on a
// tenant.
func (s *service) tenantPermission(p string) bool {
	typ, name, _ := strings.Cut(p, ".")
	for _, t := range authz.ResourceTypes.All(s.pol.Registry()) {
		if t.ID != typ {
			continue
		}
		for _, a := range t.Actions {
			if a.Name == name {
				return (a.On == "" || a.On == authz.OnTenant) && a.Class != authz.ClassSelf
			}
		}
	}
	return false
}
