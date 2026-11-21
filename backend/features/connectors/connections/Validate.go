package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Proposed is a connection as a create or an update would leave it, for a
// Validator: its settings without secrets, and which secrets are set.
type Proposed struct {
	ID       string
	Type     string
	Owner    string // tenant, or user:<id>
	Group    string
	Settings map[string]any
	Secrets  []string
}

// Validator checks what a type's settings schema cannot express, as part
// of connection.create and connection.update (forge 07f: a user's own mail
// account may not send from the tenant's mail domains, a tenant account
// sends only from its own). It runs in the command's decision, after the
// schema check, as the command's principal; problems refuse the command.
type Validator struct {
	extensions.Descriptor
	// Types are the connector types it checks; every type when empty.
	Types    []string
	Validate func(args features.ServiceArgs, c Proposed) []FieldProblem
}

// Validators is the point connection validators are registered on.
var Validators = extensions.NewPoint[Validator]("connection.validator", "Checks of a connection's settings beyond its type's schema, run by connection.create and connection.update")

// validate runs the registered validators that check c's type.
func validate(reg *extensions.Registry, args features.ServiceArgs, c Proposed) []live.Problem {
	if reg == nil {
		return nil
	}
	var out []FieldProblem
	for _, v := range Validators.All(reg) {
		if v.Validate != nil && (len(v.Types) == 0 || slices.Contains(v.Types, c.Type)) {
			out = append(out, v.Validate(args, c)...)
		}
	}
	return problemsOf(out)
}

// RefItem is the item a ref setting names ("<section>#<arrayPath>", the
// value the item's key: the array schema's key, or "id"): read from the
// section as the tenant resolves it, as a service principal. found is
// false when no item has that key.
func RefItem(ctx context.Context, r config.Reader, reg *extensions.Registry, tenant uint64, ref, value string) (item map[string]any, found bool, err error) {
	section, arrayPath, ok := strings.Cut(ref, "#")
	if !ok || section == "" || r == nil {
		return nil, false, fmt.Errorf("ref %q cannot be read", ref)
	}
	key := "id"
	if reg != nil {
		all, _ := config.SectionsFor(ctx, reg, tenant)
		for _, sec := range all {
			if sec.ID != section {
				continue
			}
			if sch, err := config.ParseSchema(sec.Schema); err == nil {
				for _, part := range strings.Split(arrayPath, ".") {
					if sch != nil && part != "" {
						sch = sch.Properties[part]
					}
				}
				if sch != nil && sch.Key != "" {
					key = sch.Key
				}
			}
		}
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalService, TenantID: tenant}}
	raw, err := r.Resolved(args, section)
	if err != nil {
		return nil, false, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	for _, part := range strings.Split(arrayPath, ".") {
		if part != "" {
			m, _ := v.(map[string]any)
			v = m[part]
		}
	}
	items, _ := v.([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if k, _ := m[key].(string); k != "" && k == value {
			return m, true, nil
		}
	}
	return nil, false, nil
}

// resolveRefs puts the item each ref setting names in place of its key,
// so an adapter in any process gets whole settings (forge 07f: a mail
// account's domain with its servers). A ref to an item that is gone is an
// error.
func resolveRefs(ctx context.Context, d Deps, tenant uint64, typ string, out map[string]any) error {
	if d.Registry == nil || d.Config == nil {
		return nil
	}
	t, ok, err := d.Registry.Type(ctx, tenant, typ)
	if err != nil || !ok {
		return err
	}
	f, err := FieldsOf(t.Type.Settings)
	if err != nil {
		return err
	}
	for k, p := range f.Schema.Properties {
		v, _ := out[k].(string)
		if p.Ref == "" || v == "" {
			continue
		}
		item, found, err := RefItem(ctx, d.Config, d.Extensions, tenant, p.Ref, v)
		if err != nil {
			return err
		}
		if !found {
			return log.Code("ref_not_found").Errorf("setting %s names %q, which is not in %s", k, v, p.Ref)
		}
		out[k] = item
	}
	return nil
}
