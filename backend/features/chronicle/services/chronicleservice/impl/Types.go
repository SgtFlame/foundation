package impl

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
)

// Types returns the built-in entry types, the tenant's and those of the
// projects the principal reads, with the starter sets.
func (s *service) Types(args chronicleservice.TypesArgs) chronicleservice.TypesResponse {
	if failed := check(args.ServiceArgs, "chronicle.read", 0); failed != nil {
		return chronicleservice.TypesResponse{Common: *failed}
	}
	types, err := chronicle.TypesIn(s.db.WithContext(args.Context()), args.Principal.TenantID, chronicle.ReadableProjects(args.Context(), args.Principal))
	if err != nil {
		return chronicleservice.TypesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.TypesResponse{Common: features.OK(), Types: types, Starters: chronicle.Starters}
}

// mayDefine checks that the principal may define types in scope: the
// tenant's with chronicle.defineTypes, a project's only when they also
// read the project.
func mayDefine(args features.ServiceArgs, scope string) *features.ServiceResponse {
	res := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, "chronicle.defineTypes", res).Err("chronicle.defineTypes", res); err != nil {
		return args.Fail(authz.Status, err)
	}
	if scope != chronicle.ScopeTenant && !slices.Contains(chronicle.ReadableProjects(args.Context(), args.Principal), scope) {
		return args.Fail(http.StatusNotFound, log.Code("project_not_found").Errorf("there is no project %q", scope))
	}
	return nil
}

// SaveType makes or replaces a type of the tenant or of a project.
func (s *service) SaveType(args chronicleservice.SaveTypeArgs) chronicleservice.SaveTypeResponse {
	t := args.Type
	problems := chronicle.CheckType(&t)
	if len(problems) > 0 {
		return chronicleservice.SaveTypeResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_type").New(problems[0].Path+": "+problems[0].Message)), Problems: problems}
	}
	if failed := mayDefine(args.ServiceArgs, t.Scope); failed != nil {
		return chronicleservice.SaveTypeResponse{Common: *failed}
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		_, err := saveType(tx, args.Principal, t, "")
		return err
	})
	if err != nil {
		return chronicleservice.SaveTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	t.Custom = true
	return chronicleservice.SaveTypeResponse{Common: features.OK(), Type: t}
}

// saveType writes a checked type, replacing the scope's type of its id
// unless keep (then it reports false and writes nothing).
func saveType(tx *gorm.DB, p features.Principal, t chronicle.EntryType, starter string, keep ...bool) (bool, error) {
	fields, _ := json.Marshal(t.RecordFields)
	parents, _ := json.Marshal(t.ParentTypes)
	if t.ParentTypes == nil {
		parents = []byte("[]")
	}
	var def chronicle.TypeDef
	err := tx.Scopes(db.InTenant(p.TenantID)).Where("type_id = ? AND scope = ?", t.ID, t.Scope).First(&def).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		def = chronicle.TypeDef{Tenanted: db.TenantOf(p), TypeID: t.ID, Scope: t.Scope, Starter: starter}
	case err != nil:
		return false, err
	case len(keep) > 0 && keep[0]:
		return false, nil
	}
	def.Title, def.Icon, def.Description, def.Fields, def.ParentTypes = t.Title, t.Icon, t.Description, string(fields), string(parents)
	return true, tx.Save(&def).Error
}

// DeleteType removes a type no entry uses (409 type_in_use).
func (s *service) DeleteType(args chronicleservice.DeleteTypeArgs) chronicleservice.DeleteTypeResponse {
	if !chronicle.ValidScope(args.Scope) {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_scope").Errorf("%q is not tenant or project:<page>", args.Scope))}
	}
	if failed := mayDefine(args.ServiceArgs, args.Scope); failed != nil {
		return chronicleservice.DeleteTypeResponse{Common: *failed}
	}
	gdb := func() *gorm.DB { return s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)) }
	var def chronicle.TypeDef
	err := gdb().Where("type_id = ? AND scope = ?", args.ID, args.Scope).First(&def).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusNotFound, log.Code("type_not_found").Errorf("there is no type %q in %s", args.ID, args.Scope))}
	}
	if err != nil {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	used := gdb().Model(&chronicle.Entry{}).Where("type = ?", args.ID)
	if args.Scope != chronicle.ScopeTenant {
		used = used.Where("scope = ?", args.Scope)
	}
	var n int64
	if err := used.Count(&n).Error; err != nil {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if n > 0 {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusConflict, log.Code("type_in_use").Errorf("%d entries are of type %q", n, args.ID))}
	}
	if err := gdb().Delete(&def).Error; err != nil {
		return chronicleservice.DeleteTypeResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.DeleteTypeResponse{Common: features.OK()}
}

// CopyStarter copies a starter set's types into a scope, keeping the
// types it already has, and returns the types as Types does.
func (s *service) CopyStarter(args chronicleservice.CopyStarterArgs) chronicleservice.TypesResponse {
	starter, ok := chronicle.StarterOf(args.Starter)
	if !ok {
		return chronicleservice.TypesResponse{Common: *args.Fail(http.StatusNotFound, log.Code("starter_not_found").Errorf("there is no starter set %q", args.Starter))}
	}
	if !chronicle.ValidScope(args.Scope) {
		return chronicleservice.TypesResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("invalid_scope").Errorf("%q is not tenant or project:<page>", args.Scope))}
	}
	if failed := mayDefine(args.ServiceArgs, args.Scope); failed != nil {
		return chronicleservice.TypesResponse{Common: *failed}
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		for _, t := range starter.Types {
			t.Scope = args.Scope
			t.RecordFields = slices.Clone(t.RecordFields)
			if problems := chronicle.CheckType(&t); len(problems) > 0 {
				return log.Errorf("starter %s: type %s: %s: %s", starter.ID, t.ID, problems[0].Path, problems[0].Message)
			}
			if _, err := saveType(tx, args.Principal, t, starter.ID, true); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return chronicleservice.TypesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return s.Types(chronicleservice.TypesArgs{ServiceArgs: args.ServiceArgs})
}
