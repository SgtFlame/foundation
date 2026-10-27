package impl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"gorm.io/gorm"
)

// Set validates a merge patch against the section's schema, stores the
// scope's new overrides and a revision, and publishes the change.
func (s *service) Set(args configservice.SetArgs) configservice.SetResponse {
	layers, f := chain(args.Principal, args.Scope)
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	sec, f := s.section(args.Section)
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	patch, err := config.Decode(args.Patch)
	if err != nil {
		return configservice.SetResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_patch").Wrapf(err, "the patch is not a JSON object"))}
	}

	// Every patched leaf must exist and take values at this scope. Plain
	// settings need config.write, and secrets their own check (checkSecrets),
	// so an Application Administrator can set the server's keys without
	// changing its other settings
	scope := layers[len(layers)-1]
	var errs []config.FieldError
	secrets, plain := false, len(patch) == 0
	patchLeaves(sec.schema, patch, "", func(path string, leaf *config.Schema) {
		secret := leaf != nil && leaf.Type == "secret"
		secrets, plain = secrets || secret, plain || !secret
		switch {
		case leaf == nil:
			errs = append(errs, config.FieldError{Path: path, Message: "is not a known setting"})
		case !allows(sec, path, string(scope.kind)):
			errs = append(errs, config.FieldError{Path: path, Message: fmt.Sprintf("cannot be set at the %s scope", scope.kind)})
		}
	})
	if plain {
		f = check(args.ServiceArgs, "config.write", args.Section, layers)
	}
	if f == nil && secrets {
		f = checkSecrets(args.ServiceArgs, args.Section, layers)
	}
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	if len(errs) > 0 {
		return invalid(args, errs)
	}

	row, overrides, f := s.load(sec, scope.key)
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	var revision int64
	if row != nil {
		revision = row.Revision
	}
	if args.Revision != revision {
		return configservice.SetResponse{Common: *args.Fail(http.StatusConflict, log.Code("config_conflict").With("section", sec.ID, "revision", revision).
			Errorf("%s was changed elsewhere (revision %d, not %d)", sec.Title, revision, args.Revision))}
	}

	next := config.Merge(overrides, patch)
	value, sources, _, f := s.resolved(sec, layers, next)
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	errs = sec.schema.Validate(value)
	if len(errs) == 0 && sec.Check != nil {
		errs = sec.Check(value)
	}
	errs = append(errs, s.refErrors(args.ServiceArgs, sec, value)...)
	if len(errs) > 0 {
		return invalid(args, errs)
	}

	row, f = s.store(args.ServiceArgs, sec, scope.key, row, overrides, next)
	if f != nil {
		return configservice.SetResponse{Common: *args.Fail(f.status, f.err)}
	}
	s.changed(sec.ID, scope.key)
	log.With("section", sec.ID, "scope", scope.key, "revision", row.Revision).Info(args.Context(), "%s saved at %s", sec.ID, scope.key)
	return configservice.SetResponse{Common: features.OK(), View: s.view(sec, args.Scope, value, sources, row)}
}

func invalid(args configservice.SetArgs, errs []config.FieldError) configservice.SetResponse {
	first := errs[0]
	err := log.Code("invalid_settings").With("section", args.Section, "errors", len(errs)).
		Errorf("%s %s", first.Path, first.Message)
	return configservice.SetResponse{Common: *args.Fail(http.StatusBadRequest, err), Errors: errs}
}

// patchLeaves calls fn for each leaf a merge patch sets or clears; leaf is
// nil for a path the schema does not have.
func patchLeaves(schema *config.Schema, patch map[string]any, path string, fn func(string, *config.Schema)) {
	for k, v := range patch {
		p := config.Join(path, k)
		child := schema.Properties[k]
		if sub, ok := v.(map[string]any); ok && child != nil && child.Type == "object" {
			patchLeaves(child, sub, p, fn)
			continue
		}
		fn(p, child)
	}
}

// refErrors checks that every ref names an item of the section it refers to,
// as the same principal sees it.
func (s *service) refErrors(args features.ServiceArgs, sec *section, value map[string]any) []config.FieldError {
	var errs []config.FieldError
	keys := map[string]map[string]bool{} // by ref
	walkRefs(sec.schema, value, "", func(path string, leaf *config.Schema, v string) {
		if _, ok := keys[leaf.Ref]; !ok {
			keys[leaf.Ref] = s.refKeys(args, sec, value, leaf.Ref)
		}
		if v != "" && !keys[leaf.Ref][v] {
			errs = append(errs, config.FieldError{Path: path, Message: fmt.Sprintf("names %q, which does not exist", v)})
		}
	})
	return errs
}

// refKeys are the keys of the items a ref ("<section>#<arrayPath>") can name.
func (s *service) refKeys(args features.ServiceArgs, sec *section, value map[string]any, ref string) map[string]bool {
	target, path, _ := strings.Cut(ref, "#")
	other, otherSchema := value, sec.schema
	if target != sec.ID {
		raw, err := s.Resolved(args, target)
		if err != nil {
			return nil
		}
		if other, err = config.Decode(raw); err != nil {
			return nil
		}
		t, f := s.section(target)
		if f != nil {
			return nil
		}
		otherSchema = t.schema
	}
	keyName := "id"
	if arr := otherSchema.At(path); arr != nil {
		keyName = arr.KeyName()
	}
	out := map[string]bool{}
	items, _ := config.Get(other, path)
	list, _ := items.([]any)
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok {
			if k, ok := obj[keyName].(string); ok {
				out[k] = true
			}
		}
	}
	return out
}

func walkRefs(schema *config.Schema, value any, path string, fn func(string, *config.Schema, string)) {
	switch {
	case schema == nil:
	case schema.Ref != "":
		if v, ok := value.(string); ok {
			fn(path, schema, v)
		}
	case schema.Type == "object":
		obj, _ := value.(map[string]any)
		for k, v := range obj {
			walkRefs(schema.Properties[k], v, config.Join(path, k), fn)
		}
	case schema.Type == "array":
		list, _ := value.([]any)
		for i, v := range list {
			walkRefs(schema.Items, v, config.Join(path, fmt.Sprint(i)), fn)
		}
	}
}

// store writes a scope's overrides, secrets sealed, as the next revision;
// prev are its plain overrides before the change. Secrets no longer set are
// deleted from the store once the row is saved.
func (s *service) store(args features.ServiceArgs, sec *section, scope string, row *config.Configuration, prev, overrides map[string]any) (*config.Configuration, *failure) {
	sealed, gone, err := s.seal(args.CallContext(), sec, scope, row, prev, overrides)
	if err != nil {
		return nil, fail(http.StatusInternalServerError, log.Wrap(err))
	}
	raw, err := json.Marshal(sealed)
	if err != nil {
		return nil, fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if row == nil {
		row = &config.Configuration{Scope: scope, Section: sec.ID}
	}
	row.Value = string(raw)
	row.SchemaVersion = sec.Version
	row.Revision++
	row.UpdatedBy = args.Principal.UserID
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(row).Error; err != nil {
			return err
		}
		return tx.Create(&config.ConfigurationRevision{ConfigurationID: row.ID, Revision: row.Revision, Value: row.Value, UpdatedBy: row.UpdatedBy}).Error
	})
	if err != nil {
		return nil, fail(http.StatusInternalServerError, log.Wrap(err))
	}
	for _, ref := range gone {
		if err := s.secrets.Delete(args.Context(), ref); err != nil {
			log.With("section", ref.Section, "scope", ref.Scope, "path", ref.Path).Error(args.Context(), err, "cannot delete a secret no longer set")
		}
	}
	return row, nil
}
