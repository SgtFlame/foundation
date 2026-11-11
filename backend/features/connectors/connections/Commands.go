package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/project"
)

// Deps are what connections need, made after the registry.
type Deps struct {
	Projections *project.Projections
	Bus         *bus.Bus
	Registry    *connectors.Registry
	Secrets     config.SecretStore
	// Dispatcher sends connection.test's check.
	Dispatcher *connectors.Dispatcher
}

// Events.
const (
	EventCreated       = "created"
	EventUpdated       = "updated"
	EventRenamed       = "renamed"
	EventDisabled      = "disabled"
	EventEnabled       = "enabled"
	EventDeleted       = "deleted"
	EventStatusChanged = "statusChanged"
)

// updated is an update event's data: settings as a merge patch (nil
// removes), secrets' references (nil clears), the options when given, and
// whether the error status is cleared.
type updated struct {
	Settings map[string]any     `json:"settings,omitempty"`
	Secrets  map[string]*string `json:"secrets,omitempty"`
	Options  *Options           `json:"options,omitempty"`
	Reset    bool               `json:"reset,omitempty"`
}

type statusChanged struct {
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

// Get reads a connection.
func Get(ctx context.Context, store live.Store, tenant uint64, id string) (Connection, bool, error) {
	if !live.ValidToken(id) {
		return Connection{}, false, nil
	}
	v, ok, err := store.Get(ctx, tenant, Projector, TargetType+"."+id)
	if err != nil || !ok {
		return Connection{}, false, err
	}
	var c Connection
	if err := json.Unmarshal(v.Value, &c); err != nil {
		return Connection{}, false, err
	}
	return c, true, nil
}

func fold(f live.Fold) (json.RawMessage, error) {
	var c Connection
	if len(f.State) > 0 {
		if err := json.Unmarshal(f.State, &c); err != nil {
			return nil, err
		}
	}
	e := f.Event
	switch e.Type {
	case EventCreated:
		if err := json.Unmarshal(e.Data, &c); err != nil {
			return nil, err
		}
	case EventUpdated:
		var u updated
		if err := json.Unmarshal(e.Data, &u); err != nil {
			return nil, err
		}
		if c.Settings == nil {
			c.Settings = map[string]any{}
		}
		for k, v := range u.Settings {
			if v == nil {
				delete(c.Settings, k)
			} else {
				c.Settings[k] = v
			}
		}
		for k, v := range u.Secrets {
			if c.Secrets == nil {
				c.Secrets = map[string]string{}
			}
			if v == nil {
				delete(c.Secrets, k)
			} else {
				c.Secrets[k] = *v
			}
		}
		if len(c.Secrets) == 0 {
			c.Secrets = nil
		}
		if u.Options != nil {
			c.Options = *u.Options
		}
		if u.Reset && c.Status == StatusError {
			c.Status, c.LastError = StatusOK, ""
		}
	case EventRenamed:
		var r struct{ Name string }
		_ = json.Unmarshal(e.Data, &r)
		c.Name = r.Name
	case EventDisabled:
		c.Status = StatusDisabled
	case EventEnabled:
		c.Status, c.LastError = StatusOK, ""
	case EventStatusChanged:
		var s statusChanged
		_ = json.Unmarshal(e.Data, &s)
		if c.Status != StatusDisabled {
			c.Status = s.Status
		}
		c.LastError, c.CheckedAt = s.Error, s.At
	case EventDeleted:
		return nil, nil
	default:
		return f.State, nil
	}
	c.UpdatedAt = e.Time
	return json.Marshal(c)
}

const argsDescriptions = `
  "settings":{"type":"object","description":"The connection's settings, by the connector type's settings schema (connectors.describe connector:<type>), without its secrets"},
  "secrets":{"type":"object","description":"The secret fields of the settings schema, field → value; write-only: a value replaces the secret, an empty string keeps it, null clears it"},
  "options":{"type":"object","description":"Event source options: {pollInterval (a duration such as 5m, for a poll connector), since (YYYY-MM-DD: how far back the first pass reads), paused}","properties":{"pollInterval":{"type":"string","description":"How long a poll connector waits between passes"},"since":{"type":"string","description":"The initial window's first day, YYYY-MM-DD"},"paused":{"type":"boolean","description":"Stops the event source while true"}}}`

// Contribute registers the connection commands, the projection, the
// queries and connection.test.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "connectors"}, Title: "Connections",
		Description: "The tenant's connections by id: type, owner, settings without secrets (each secret only a reference), status and event source options",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	command := func(c live.Command) {
		c.Descriptor.Source = "connectors"
		c.Area, c.TargetType, c.Projection = live.AreaDefinitions, TargetType, Projector
		if c.Action == "" {
			c.Action = "connection.read" // the decision checks the connection's own access
		}
		live.Commands.Register(reg, c)
	}
	command(live.Command{
		Descriptor: extensions.Descriptor{ID: "connection.create"}, Title: "Create a connection",
		Description: "Creates a connection: one configured use of a connector type (a mailbox over IMAP at a host, with its password). owner me (the default) makes it yours alone; tenant makes it the tenant's, which needs connection.manage on the tenant. Its secrets are stored apart and never shown again.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "type":{"type":"string","description":"The connector type, such as mail.imap (connectors.types)"},
  "name":{"type":"string","description":"A name people know it by, such as Support inbox"},
  "owner":{"type":"string","enum":["me","tenant"],"description":"me (yours alone, the default) or tenant"},` + argsDescriptions + `},"required":["type","name"]}`),
		Examples:  []json.RawMessage{json.RawMessage(`{"type":"test.secure","name":"Support","settings":{"user":"support"},"secrets":{"password":"…"}}`)},
		Events:    []string{EventCreated},
		NewTarget: func() string { return strconv.FormatUint(features.NewTSID(), 10) },
		NoTool:    true, // its secrets would be recorded with the tool call
		Seal:      seal(deps, true),
		Decide:    func(d live.Decision) ([]live.NewEvent, []live.Problem) { return decideCreate(deps(), d) },
	})
	command(live.Command{
		Descriptor: extensions.Descriptor{ID: "connection.update"}, Title: "Update a connection",
		Description: "Changes a connection's settings (a merge: null removes a setting), secrets (a value replaces one, an empty string keeps it) and event source options. An errored connection is tried again.",
		Args:        json.RawMessage(`{"type":"object","properties":{` + strings.TrimPrefix(argsDescriptions, "\n") + `}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"settings":{"user":"help"},"secrets":{"password":""}}`)},
		Events:      []string{EventUpdated},
		NoTool:      true,
		Seal:        seal(deps, false),
		Decide:      func(d live.Decision) ([]live.NewEvent, []live.Problem) { return decideUpdate(deps(), d) },
	})
	command(live.Command{
		Descriptor: extensions.Descriptor{ID: "connection.rename"}, Title: "Rename a connection",
		Description: "Renames a connection; its id, and every record and trigger naming it, stay.",
		Args:        json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"The new name"}},"required":["name"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"name":"Help desk"}`)},
		Events:      []string{EventRenamed},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			c, p := managed(d)
			if p != nil {
				return nil, p
			}
			var a struct{ Name string }
			_ = json.Unmarshal(d.Args, &a)
			if strings.TrimSpace(a.Name) == "" {
				return nil, []live.Problem{{Path: "name", Message: "a connection needs a name"}}
			}
			d.Undo("", live.OpOf("connection.rename", c.ID, map[string]any{"name": c.Name}))
			return []live.NewEvent{{Type: EventRenamed, Data: map[string]any{"name": strings.TrimSpace(a.Name)}}}, nil
		},
	})
	toggle := func(id, title, description, event, inverse string) {
		command(live.Command{
			Descriptor: extensions.Descriptor{ID: id}, Title: title, Description: description,
			Args: json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
			Events: []string{event},
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c, p := managed(d)
				if p != nil {
					return nil, p
				}
				d.Undo("", live.OpOf(inverse, c.ID, map[string]any{}))
				return []live.NewEvent{{Type: event, Data: map[string]any{}}}, nil
			},
		})
	}
	toggle("connection.disable", "Disable a connection", "Disables a connection: calls on it are refused and its event source stops, until connection.enable.", EventDisabled, "connection.enable")
	toggle("connection.enable", "Enable a connection", "Enables a disabled or errored connection again.", EventEnabled, "connection.disable")
	command(live.Command{
		Descriptor: extensions.Descriptor{ID: "connection.delete"}, Title: "Delete a connection",
		Description: "Deletes a connection and forgets its secrets; records naming it keep the id, which no longer resolves. Cannot be undone.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventDeleted},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if _, p := managed(d); p != nil {
				return nil, p
			}
			d.NoUndo("", "a deleted connection's secrets are forgotten")
			return []live.NewEvent{{Type: EventDeleted, Data: map[string]any{}}}, nil
		},
	})
	command(live.Command{
		Descriptor: extensions.Descriptor{ID: "connection.setStatus"}, Title: "Set a connection's status", Internal: true,
		Description: "Records a connection's status from a call or its event source: error stops it until it is updated; ok with an error records the last error. Committed by the server only.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "status":{"type":"string","enum":["ok","error"],"description":"ok or error"},
  "error":{"type":"string","description":"The last error, empty for none"}},"required":["status"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"status":"error","error":"auth: the password was refused"}`)},
		Events:   []string{EventStatusChanged},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if len(d.State) == 0 {
				return nil, []live.Problem{{Code: "connection_not_found", Message: fmt.Sprintf("there is no connection %s", d.Target.ID)}}
			}
			var a statusChanged
			_ = json.Unmarshal(d.Args, &a)
			a.At = time.Now().UTC()
			d.NoUndo("", "a status is the system's")
			return []live.NewEvent{{Type: EventStatusChanged, Data: a}}, nil
		},
	})
	contributeQueries(reg, deps)
}

// managed reads the decided connection and checks manage on it.
func managed(d live.Decision) (Connection, []live.Problem) {
	if len(d.State) == 0 {
		return Connection{}, []live.Problem{{Code: "connection_not_found", Message: fmt.Sprintf("there is no connection %s", d.Target.ID)}}
	}
	var c Connection
	if err := json.Unmarshal(d.State, &c); err != nil {
		return c, []live.Problem{{Message: err.Error()}}
	}
	if err := CheckAccess(d.ServiceArgs, c, "manage"); err != nil {
		return c, []live.Problem{{Code: "forbidden", Message: err.Error()}}
	}
	return c, nil
}

// CheckAccess checks an action on a connection as the principal: a user's
// own connection is used and managed by them alone (an administrator sees
// that it exists), a tenant connection by the grants on it.
func CheckAccess(args features.ServiceArgs, c Connection, action string) error {
	if u := UserOf(c.Owner); u != 0 && action != "read" && !(args.Principal.Kind == features.PrincipalUser && args.Principal.UserID == u) &&
		args.Principal.Kind != features.PrincipalService {
		return log.Code("forbidden").Errorf("connection %s is another user's own", c.ID)
	}
	return Check(args, c, action)
}

// argsOf are a create's or an update's arguments after Seal.
type argsOf struct {
	Type     string                     `json:"type"`
	Name     string                     `json:"name"`
	Owner    string                     `json:"owner"`
	Settings map[string]any             `json:"settings"`
	Secrets  map[string]json.RawMessage `json:"secrets"`
	Options  *Options                   `json:"options"`
}

// sealedSecrets are the references Seal left: field → version, or nil.
func sealedSecrets(raw map[string]json.RawMessage) (map[string]*string, []live.Problem) {
	out := map[string]*string{}
	for k, v := range raw {
		if string(v) == "null" {
			out[k] = nil
			continue
		}
		var ref map[string]string
		if json.Unmarshal(v, &ref) != nil || ref[refKey] == "" {
			return nil, []live.Problem{{Path: "secrets." + k, Message: "a secret must be sealed before it is decided"}}
		}
		version := ref[refKey]
		out[k] = &version
	}
	return out, nil
}

func fieldsFor(d Deps, args features.ServiceArgs, typ string) (connectors.Type, Fields, []live.Problem) {
	if d.Registry == nil {
		return connectors.Type{}, Fields{}, []live.Problem{{Message: "there is no connector registry"}}
	}
	t, ok, err := d.Registry.Type(args.Context(), args.Principal.TenantID, typ)
	if err != nil {
		return connectors.Type{}, Fields{}, []live.Problem{{Message: err.Error()}}
	}
	if !ok {
		return connectors.Type{}, Fields{}, []live.Problem{{Path: "type", Message: fmt.Sprintf("there is no connector type %q", typ), Fix: "list them with connectors.types"}}
	}
	if t.Type.NoConnection {
		return connectors.Type{}, Fields{}, []live.Problem{{Path: "type", Message: fmt.Sprintf("%s needs no connection", typ)}}
	}
	f, err := FieldsOf(t.Type.Settings)
	if err != nil {
		return connectors.Type{}, Fields{}, []live.Problem{{Path: "type", Message: fmt.Sprintf("the settings schema of %s cannot be read: %v", typ, err)}}
	}
	return t.Type, f, nil
}

func problemsOf(fp []FieldProblem) []live.Problem {
	out := make([]live.Problem, len(fp))
	for i, p := range fp {
		out[i] = live.Problem{Path: p.Path, Message: p.Message}
	}
	return out
}

func decideCreate(deps Deps, d live.Decision) ([]live.NewEvent, []live.Problem) {
	if len(d.State) > 0 {
		return nil, []live.Problem{{Code: "connection_exists", Message: fmt.Sprintf("connection %s exists", d.Target.ID)}}
	}
	var a argsOf
	if err := json.Unmarshal(d.Args, &a); err != nil {
		return nil, []live.Problem{{Message: err.Error()}}
	}
	owner := OwnerOf(d.Principal)
	if a.Owner == OwnerTenant || d.Principal.Kind != features.PrincipalUser {
		owner = OwnerTenant
	}
	c := Connection{ID: d.Target.ID, Name: strings.TrimSpace(a.Name), Type: a.Type, Owner: owner, Settings: a.Settings, Status: StatusOK,
		CreatedAt: time.Now().UTC(), CreatedBy: d.Principal.UserID}
	if owner == OwnerTenant {
		res := authz.Tenant(d.Principal)
		if err := authz.Check(d.Context(), d.Principal, "connection.manage", res).Err("connection.manage", res); err != nil {
			return nil, []live.Problem{{Code: "forbidden", Path: "owner", Message: err.Error(), Fix: "make it yours (owner me), or ask an administrator"}}
		}
	}
	if c.Name == "" {
		return nil, []live.Problem{{Path: "name", Message: "a connection needs a name"}}
	}
	if c.Settings == nil {
		c.Settings = map[string]any{}
	}
	_, fields, p := fieldsFor(deps, d.ServiceArgs, a.Type)
	if p != nil {
		return nil, p
	}
	secrets, p := sealedSecrets(a.Secrets)
	if p != nil {
		return nil, p
	}
	set := map[string]string{}
	for k, v := range secrets {
		if v != nil {
			set[k] = *v
		}
	}
	if fp := fields.Validate(c.Settings, set); len(fp) > 0 {
		return nil, problemsOf(fp)
	}
	if len(set) > 0 {
		c.Secrets = set
	}
	if a.Options != nil {
		if fp := checkOptions(*a.Options); len(fp) > 0 {
			return nil, problemsOf(fp)
		}
		c.Options = *a.Options
	}
	d.Undo("", live.OpOf("connection.delete", c.ID, map[string]any{}))
	return []live.NewEvent{{Type: EventCreated, Data: c}}, nil
}

func decideUpdate(deps Deps, d live.Decision) ([]live.NewEvent, []live.Problem) {
	c, p := managed(d)
	if p != nil {
		return nil, p
	}
	var a argsOf
	if err := json.Unmarshal(d.Args, &a); err != nil {
		return nil, []live.Problem{{Message: err.Error()}}
	}
	_, fields, p := fieldsFor(deps, d.ServiceArgs, c.Type)
	if p != nil {
		return nil, p
	}
	secrets, p := sealedSecrets(a.Secrets)
	if p != nil {
		return nil, p
	}
	// The connection as it will be, checked whole
	settings := maps.Clone(c.Settings)
	if settings == nil {
		settings = map[string]any{}
	}
	for k, v := range a.Settings {
		if v == nil {
			delete(settings, k)
		} else {
			settings[k] = v
		}
	}
	set := maps.Clone(c.Secrets)
	if set == nil {
		set = map[string]string{}
	}
	for k, v := range secrets {
		if v == nil {
			delete(set, k)
			continue
		}
		if !hasSecret(d.Context(), deps, d.Principal.TenantID, c.ID, k, *v) {
			return nil, []live.Problem{{Path: "secrets." + k, Message: "names a secret version that is not stored"}}
		}
		set[k] = *v
	}
	if fp := fields.Validate(settings, set); len(fp) > 0 {
		return nil, problemsOf(fp)
	}
	if a.Options != nil {
		if fp := checkOptions(*a.Options); len(fp) > 0 {
			return nil, problemsOf(fp)
		}
	}
	u := updated{Settings: a.Settings, Secrets: secrets, Options: a.Options, Reset: true}
	if len(u.Secrets) == 0 {
		u.Secrets = nil
	}
	// The inverse: what each changed part was
	inv := map[string]any{}
	if len(a.Settings) > 0 {
		old := map[string]any{}
		for k := range a.Settings {
			old[k] = c.Settings[k] // nil removes what was not there
		}
		inv["settings"] = old
	}
	if len(secrets) > 0 {
		old := map[string]any{}
		for _, k := range slices.Sorted(maps.Keys(secrets)) {
			if v, ok := c.Secrets[k]; ok {
				old[k] = map[string]string{refKey: v}
			} else {
				old[k] = nil
			}
		}
		inv["secrets"] = old
	}
	if a.Options != nil {
		inv["options"] = c.Options
	}
	d.Undo("", live.OpOf("connection.update", c.ID, inv))
	return []live.NewEvent{{Type: EventUpdated, Data: u}}, nil
}
