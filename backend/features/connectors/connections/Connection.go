// Package connections is connections (forge 07c,
// documentation/Connectors.md › Built in slice 47): each one configured
// use of a connector type, with its settings, its secrets kept in the
// SecretStore and only referenced elsewhere, its owner, its status and
// its event source options. Connections are definitions on the def
// stream, changed by connection.* commands and read by the
// connections.list and connections.describe queries. Foundation serves
// adapters their settings on cx.resolve, commits their status from call
// errors and cx.status, and keeps cx_sources: the connections an
// installed trigger listens to.
package connections

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("connectors").Tags("connections")

// TargetType is a connection's target type on the def stream.
const TargetType = "connection"

// Projector is the connections' projection.
const Projector = "connectors.connection"

// Statuses.
const (
	StatusOK       = "ok"
	StatusError    = "error"
	StatusDisabled = "disabled"
)

// Owners.
const (
	OwnerTenant = "tenant"
	ownerUser   = "user:"
)

// Connection is one configured use of a connector type, as the projection
// holds it. Secrets are only references: the value is in the SecretStore,
// its placeholder in cx_secrets_<tenant>.
type Connection struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Owner    string            `json:"owner"` // tenant, or user:<id>
	Settings map[string]any    `json:"settings"`
	Secrets  map[string]string `json:"secrets,omitempty"` // field → version
	// Status is ok, error (an auth error stopped it until it is updated)
	// or disabled.
	Status    string    `json:"status"`
	LastError string    `json:"lastError,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	Options   Options   `json:"options"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy uint64    `json:"createdBy,string,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Options are a connection's event source options.
type Options struct {
	// PollInterval is how long a poll connector waits between passes, a
	// Go duration (1m when empty).
	PollInterval string `json:"pollInterval,omitempty"`
	// Since is the initial window: the first Watch with no cursor reads
	// events from this date (YYYY-MM-DD).
	Since  string `json:"since,omitempty"`
	Paused bool   `json:"paused,omitempty"`
}

// OwnerOf is the owner a principal means by "me".
func OwnerOf(p features.Principal) string { return ownerUser + strconv.FormatUint(p.UserID, 10) }

// UserOf is a user owner's id; 0 for the tenant.
func UserOf(owner string) uint64 {
	id, _ := strconv.ParseUint(strings.TrimPrefix(owner, ownerUser), 10, 64)
	if !strings.HasPrefix(owner, ownerUser) {
		return 0
	}
	return id
}

// Resource is a connection's authz path: tenant:<t>/connection:<id>, or
// tenant:<t>/user:<u>/connection:<id> for a user's own, which is theirs.
func Resource(tenant uint64, owner, id string) string {
	base := "tenant:" + strconv.FormatUint(tenant, 10)
	if u := UserOf(owner); u != 0 {
		base += "/user:" + strconv.FormatUint(u, 10)
	}
	return base + "/connection:" + id
}

// Check checks an action on a connection as p.
func Check(args features.ServiceArgs, c Connection, action string) error {
	res := Resource(args.Principal.TenantID, c.Owner, c.ID)
	return authz.Check(args.Context(), args.Principal, "connection."+action, res).Err("connection."+action, res)
}

// View is a connection as a client sees it: each secret only as set.
type View struct {
	Connection
	Secrets map[string]SecretView `json:"secrets"`
}

// SecretView is a secret as a client sees it.
type SecretView struct {
	Set bool `json:"set"`
}

// ViewOf is c without its secrets' references.
func ViewOf(c Connection) View {
	v := View{Connection: c, Secrets: map[string]SecretView{}}
	for f := range c.Secrets {
		v.Secrets[f] = SecretView{Set: true}
	}
	if v.Settings == nil {
		v.Settings = map[string]any{}
	}
	return v
}

// Usable reports why a connection cannot be called, "" when it can.
func Usable(c Connection) string {
	switch c.Status {
	case StatusDisabled:
		return "connection " + c.Name + " is disabled"
	case StatusError:
		msg := "connection " + c.Name + " failed to sign in"
		if c.LastError != "" {
			msg += ": " + c.LastError
		}
		return msg + "; update it to try again"
	}
	return ""
}

// Fields are a type's settings schema split in plain settings and secret
// fields (the top-level fields of type secret).
type Fields struct {
	Schema  *config.Schema
	Secrets []string
}

// FieldsOf reads a type's settings schema; a type with none takes no
// settings.
func FieldsOf(raw json.RawMessage) (Fields, error) {
	if len(raw) == 0 {
		return Fields{Schema: &config.Schema{Type: "object", Properties: map[string]*config.Schema{}}}, nil
	}
	s, err := config.ParseSchema(raw)
	if err != nil {
		return Fields{}, err
	}
	var secrets []string
	for k, p := range s.Properties {
		if p.Type == "secret" {
			secrets = append(secrets, k)
		}
	}
	slices.Sort(secrets)
	return Fields{Schema: s, Secrets: secrets}, nil
}

// Validate checks settings and the secrets that will be set against the
// schema: secret values are not given, only whether each is set.
func (f Fields) Validate(settings map[string]any, secrets map[string]string) []FieldProblem {
	var out []FieldProblem
	value := map[string]any{}
	for k, v := range settings {
		if slices.Contains(f.Secrets, k) {
			out = append(out, FieldProblem{Path: "settings." + k, Message: "is a secret: send it in secrets"})
			continue
		}
		value[k] = v
	}
	for k := range secrets {
		if !slices.Contains(f.Secrets, k) {
			out = append(out, FieldProblem{Path: "secrets." + k, Message: "is not a secret of this connector type"})
			continue
		}
		value[k] = "set"
	}
	for _, e := range f.Schema.Validate(value) {
		path := "settings." + e.Path
		if slices.Contains(f.Secrets, strings.SplitN(e.Path, ".", 2)[0]) {
			path = "secrets." + e.Path
		}
		out = append(out, FieldProblem{Path: path, Message: e.Message})
	}
	return out
}

// FieldProblem is a settings problem at a path.
type FieldProblem struct{ Path, Message string }

// checkOptions checks event source options.
func checkOptions(o Options) []FieldProblem {
	var out []FieldProblem
	if o.PollInterval != "" {
		if d, err := time.ParseDuration(o.PollInterval); err != nil || d < time.Second {
			out = append(out, FieldProblem{Path: "options.pollInterval", Message: fmt.Sprintf("%q is not an interval of at least 1s", o.PollInterval)})
		}
	}
	if o.Since != "" {
		if _, err := time.Parse("2006-01-02", o.Since); err != nil {
			out = append(out, FieldProblem{Path: "options.since", Message: fmt.Sprintf("%q is not a date (YYYY-MM-DD)", o.Since)})
		}
	}
	return out
}
