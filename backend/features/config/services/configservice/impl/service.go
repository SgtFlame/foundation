package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

var log = logging.In("config")

type service struct {
	db       *gorm.DB
	registry *extensions.Registry
	secrets  config.SecretStore
	changes  *config.Changes
	env      map[string]map[string]any // section → the flag and env values, as a partial value
	server   map[string]map[string]any // section → the server's own secrets (Vault's bootstrap bundle), under every scope

	// unreadable counts secrets the store failed to return, so a value
	// resolved while one was missing is not cached
	unreadable atomic.Int64

	mu    sync.Mutex
	cache map[string][]byte // resolved plain values, by section and scope chain
}

// failure is an error with the status to fail with.
type failure struct {
	status uint16
	err    error
}

func fail(status uint16, err error) *failure { return &failure{status, err} }

// section is a registered section with its parsed schema.
type section struct {
	config.Section
	schema   *config.Schema
	defaults map[string]any
}

func (s *service) section(id string) (*section, *failure) {
	i := slices.IndexFunc(config.Sections.All(s.registry), func(sec config.Section) bool { return sec.ID == id })
	if i < 0 {
		return nil, fail(http.StatusNotFound, log.Code("section_not_found").With("section", id).Errorf("there is no settings section %q", id))
	}
	sec := config.Sections.All(s.registry)[i]
	schema, err := config.ParseSchema(sec.Schema)
	if err != nil {
		return nil, fail(http.StatusInternalServerError, log.With("section", id).Wrapf(err, "section %s has a bad schema", id))
	}
	defaults, err := config.Decode(sec.Defaults)
	if err != nil {
		return nil, fail(http.StatusInternalServerError, log.With("section", id).Wrapf(err, "section %s has bad defaults", id))
	}
	return &section{Section: sec, schema: schema, defaults: defaults}, nil
}

// layer is one scope of a chain, e.g. {kind: "tenant", key: "tenant:1"}.
type layer struct {
	kind configservice.Scope
	key  string // the Configuration.Scope value
}

// chain is the layers seen from scope, lowest first; the last is written.
func chain(p features.Principal, scope configservice.Scope) ([]layer, *failure) {
	app := layer{configservice.ScopeApp, "app"}
	tenant := layer{configservice.ScopeTenant, "tenant:" + strconv.FormatUint(p.TenantID, 10)}
	user := layer{configservice.ScopeUser, "user:" + strconv.FormatUint(p.UserID, 10)}
	switch scope {
	case "", configservice.ScopeApp:
		return []layer{app}, nil
	case configservice.ScopeTenant:
		return []layer{app, tenant}, nil
	case configservice.ScopeUser:
		if p.UserID == 0 {
			return nil, fail(http.StatusBadRequest, log.Code("no_user").New("user settings need a user"))
		}
		return []layer{app, tenant, user}, nil
	}
	return nil, fail(http.StatusBadRequest, log.Code("unknown_scope").Errorf("unknown scope %q", scope))
}

// readerScope is the widest scope p has: what a Go caller acting for p sees.
func readerScope(p features.Principal) configservice.Scope {
	if p.UserID == 0 {
		return configservice.ScopeTenant
	}
	return configservice.ScopeUser
}

// load reads a scope's stored overrides with their secrets decrypted, and
// brought up to the section's version. row is nil when nothing is stored.
func (s *service) load(sec *section, scope string) (row *config.Configuration, overrides map[string]any, f *failure) {
	var rows []config.Configuration
	if err := s.db.Where("scope = ? AND section = ?", scope, sec.ID).Limit(1).Find(&rows).Error; err != nil {
		return nil, nil, fail(http.StatusInternalServerError, log.Wrap(err))
	}
	if len(rows) == 0 {
		return nil, map[string]any{}, nil
	}
	r := rows[0]
	overrides, err := config.Decode([]byte(r.Value))
	if err != nil {
		return nil, nil, fail(http.StatusInternalServerError, log.With("section", sec.ID, "scope", scope).Wrapf(err, "stored settings do not decode"))
	}
	if r.SchemaVersion < sec.Version && sec.Migrate != nil {
		if overrides, err = sec.Migrate(overrides, r.SchemaVersion); err != nil {
			return nil, nil, fail(http.StatusInternalServerError, log.With("section", sec.ID, "scope", scope).Wrapf(err, "cannot migrate stored settings from version %d", r.SchemaVersion))
		}
	}
	s.decrypt(sec, scope, overrides)
	return &r, overrides, nil
}

// decrypt replaces each stored secret in overrides with its plain text. One
// that cannot be read (the key changed, or Vault lost it) is dropped and
// logged, so it shows as unset and can be entered again.
func (s *service) decrypt(sec *section, scope string, overrides map[string]any) {
	sec.schema.Leaves(func(path string, leaf *config.Schema) {
		if leaf.Type != "secret" {
			return
		}
		v, ok := config.Get(overrides, path)
		if !ok {
			return
		}
		stored, _ := v.(map[string]any)
		plain, err := "", errors.New("config: the secret store is not configured")
		if s.secrets != nil && stored != nil {
			plain, err = s.secrets.Get(context.Background(), config.SecretRef{Scope: scope, Section: sec.ID, Path: path}, stored)
		}
		if err != nil {
			s.unreadable.Add(1)
			log.With("section", sec.ID, "scope", scope, "path", path).Error(nil, err, "dropping a secret that cannot be read")
			config.Delete(overrides, path)
			return
		}
		config.Set(overrides, path, plain)
	})
}

// seal returns plain overrides with each secret replaced by the store's
// placeholder. A secret unchanged since prev (the scope's plain overrides
// before this change) keeps its placeholder, so saving a section never
// writes a new version of a secret nobody changed. gone are the secrets
// prev had and next does not, to delete once the row is saved.
func (s *service) seal(ctx context.Context, sec *section, scope string, row *config.Configuration, prev, next map[string]any) (out map[string]any, gone []config.SecretRef, err error) {
	out = config.Clone(next).(map[string]any)
	var placeholders map[string]any
	if row != nil {
		placeholders, _ = config.Decode([]byte(row.Value))
	}
	sec.schema.Leaves(func(path string, leaf *config.Schema) {
		if leaf.Type != "secret" || err != nil {
			return
		}
		ref := config.SecretRef{Scope: scope, Section: sec.ID, Path: path}
		v, set := config.Get(out, path)
		old, had := config.Get(prev, path)
		if !set {
			if had {
				gone = append(gone, ref)
			}
			return
		}
		if kept, ok := config.Get(placeholders, path); ok && had && reflect.DeepEqual(old, v) && config.IsPlaceholder(kept) {
			config.Set(out, path, kept)
			return
		}
		if s.secrets == nil {
			err = errors.New("config: the secret store is not configured")
			return
		}
		var stored map[string]any
		if stored, err = s.secrets.Put(ctx, ref, fmt.Sprint(v)); err == nil {
			config.Set(out, path, stored)
		}
	})
	return out, gone, err
}

// resolved layers the section along layers, with replace standing in for
// the stored overrides of the last layer when it is not nil. It returns the
// plain value, each leaf's source, and the last layer's row.
func (s *service) resolved(sec *section, layers []layer, replace map[string]any) (map[string]any, map[string]string, *config.Configuration, *failure) {
	env := s.env[sec.ID]
	plainEnv, secretEnv := map[string]any{}, map[string]any{}
	sec.schema.Leaves(func(path string, leaf *config.Schema) {
		if v, ok := config.Get(env, path); ok {
			if leaf.Type == "secret" {
				config.Set(secretEnv, path, v)
			} else {
				config.Set(plainEnv, path, v)
			}
		}
	})

	type named struct {
		source string
		value  map[string]any
	}
	stack := []named{{configservice.SourceDefault, sec.defaults}, {configservice.SourceServer, s.server[sec.ID]}, {configservice.SourceEnv, plainEnv}}
	var last *config.Configuration
	for i, l := range layers {
		row, overrides, f := s.load(sec, l.key)
		if f != nil {
			return nil, nil, nil, f
		}
		if i == len(layers)-1 {
			last = row
			if replace != nil {
				overrides = replace
			}
		}
		stack = append(stack, named{string(l.kind), overrides})
	}
	stack = append(stack, named{configservice.SourceEnv, secretEnv})

	value := map[string]any{}
	for _, n := range stack {
		value = config.Merge(value, n.value)
	}
	sources := map[string]string{}
	sec.schema.Leaves(func(path string, _ *config.Schema) {
		for _, n := range stack {
			if _, ok := config.Get(n.value, path); ok && (n.source == configservice.SourceDefault || n.source == configservice.SourceServer || n.source == configservice.SourceEnv || allows(sec, path, n.source)) {
				sources[path] = n.source
			}
		}
	})
	return value, sources, last, nil
}

func allows(sec *section, path, scope string) bool {
	return sec.schema.AllowsScope(path, sec.Scopes, scope)
}

// view is the client's view of a resolved value: secrets redacted, with a
// hint only when the store allows one.
func (s *service) view(sec *section, scope configservice.Scope, value map[string]any, sources map[string]string, row *config.Configuration) configservice.View {
	out := config.Clone(value).(map[string]any)
	hints := s.secrets == nil || s.secrets.Hints()
	sec.schema.Leaves(func(path string, leaf *config.Schema) {
		if leaf.Type != "secret" {
			return
		}
		v, _ := config.Get(out, path)
		plain, _ := v.(string)
		redacted := map[string]any{"set": plain != ""}
		if hints {
			redacted["hint"] = config.Hint(plain)
		}
		config.Set(out, path, redacted)
	})
	v := configservice.View{Section: sec.ID, Scope: scope, Value: out, Sources: sources}
	if v.Scope == "" {
		v.Scope = configservice.ScopeApp
	}
	if row != nil {
		v.Revision = row.Revision
	}
	return v
}

// check is the authz check of every operation; resource is the section at
// the written scope, e.g. "app/config:ai.models".
func check(args features.ServiceArgs, action, sectionID string, layers []layer) *failure {
	resource := layers[len(layers)-1].key + "/config:" + sectionID
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return fail(authz.Status, err)
	}
	return nil
}

// checkSecrets is the check of a change to secrets at the written scope: an
// app-scope secret is a server secret, written by an Application
// Administrator (Secrets question 7); a tenant's or a user's needs
// config.writeSecrets there.
func checkSecrets(args features.ServiceArgs, sectionID string, layers []layer) *failure {
	if layers[len(layers)-1].kind != configservice.ScopeApp {
		return check(args, "config.writeSecrets", sectionID, layers)
	}
	const action, resource = "secret.write", "app"
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return fail(authz.Status, err)
	}
	return nil
}

// Resolved is the section as args' principal sees it, secrets in plain text.
func (s *service) Resolved(args features.ServiceArgs, sectionID string) (json.RawMessage, error) {
	layers, f := chain(args.Principal, readerScope(args.Principal))
	if f != nil {
		return nil, f.err
	}
	key := sectionID
	for _, l := range layers {
		key += "|" + l.key
	}
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	sec, f := s.section(sectionID)
	if f != nil {
		return nil, f.err
	}
	unreadable := s.unreadable.Load()
	value, _, _, f := s.resolved(sec, layers, nil)
	if f != nil {
		return nil, f.err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, log.Wrap(err)
	}
	if s.unreadable.Load() == unreadable { // a secret that could not be read is tried again next time
		s.mu.Lock()
		s.cache[key] = raw
		s.mu.Unlock()
	}
	return raw, nil
}

// changed drops cached values and tells subscribers.
func (s *service) changed(sectionID, scope string) {
	s.mu.Lock()
	for k := range s.cache {
		if strings.HasPrefix(k, sectionID+"|") {
			delete(s.cache, k)
		}
	}
	s.mu.Unlock()
	s.changes.Publish(config.Changed{Section: sectionID, Scope: scope})
}
