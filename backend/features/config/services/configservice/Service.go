package configservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
)

// Service stores and resolves the settings sections registered on
// config.Sections (documentation/plans/Configuration.md). Values are layered
// defaults → the server's secrets → env (non-secret keys) → app → tenant →
// user, and flag or env
// values win over everything for secrets. Resolved, from config.Reader, is
// for Go callers; every other method is served over REST and never returns a
// secret.
type Service interface {
	config.Reader
	Sections(args SectionsArgs) SectionsResponse
	Get(args GetArgs) GetResponse
	Set(args SetArgs) SetResponse
	Reset(args ResetArgs) ResetResponse
	Watch(args WatchArgs) *WatchRequest
}

// A Scope names a layer relative to the principal: "app", or the principal's
// own "tenant" or "user". Requests never carry tenant or user IDs.
type Scope string

const (
	ScopeApp    Scope = "app"
	ScopeTenant Scope = "tenant"
	ScopeUser   Scope = "user"
)

// Sources of a resolved leaf, lowest precedence first: the default, the
// server's own secret from Vault, a flag or env var (for a secret, it wins
// over every scope too), then the scopes.
const (
	SourceDefault = "default"
	SourceServer  = "server"
	SourceEnv     = "env"
)

type SectionsArgs struct {
	features.ServiceArgs
}

type SectionsResponse struct {
	Common   features.ServiceResponse
	Sections []config.Section
}

type GetArgs struct {
	features.ServiceArgs
	Section string
	Scope   Scope
}

// View is a section resolved as seen from a scope. Secrets appear as
// {"set": bool, "hint": "…f3a9"}, never in plain text, and with no hint
// when Vault holds them.
type View struct {
	Section  string
	Scope    Scope
	Value    map[string]any
	Sources  map[string]string // by leaf path: default, server, env, app, tenant or user
	Revision int64             // of the scope's own stored overrides; 0 when there are none
}

type GetResponse struct {
	Common features.ServiceResponse
	View
}

// SetArgs applies Patch, a JSON merge patch, to Scope's overrides: a key set
// to null is inherited again, arrays are replaced whole, and a secret is
// replaced by a string. Revision must be the View's, or Set fails with 409.
type SetArgs struct {
	features.ServiceArgs
	Section  string
	Scope    Scope
	Patch    json.RawMessage
	Revision int64
}

type SetResponse struct {
	Common features.ServiceResponse
	View
	Errors []config.FieldError // when the patched value is invalid (400)
}

// ResetArgs drops Scope's overrides of Keys (leaf paths), or of every key
// when Keys is empty, so their values are inherited again.
type ResetArgs struct {
	features.ServiceArgs
	Section string
	Scope   Scope
	Keys    []string
}

type ResetResponse struct {
	Common features.ServiceResponse
	View
}

type WatchArgs struct {
	features.ServiceArgs
}

// WatchRequest delivers every change until the args' context ends, then
// closes Events.
type WatchRequest struct {
	Events <-chan config.Changed
}
