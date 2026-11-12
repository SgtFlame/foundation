package connections_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// profiles is a config.Reader holding the test.profiles section.
type profiles struct{ value string }

func (p profiles) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if section != "test.profiles" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(p.value), nil
}

// groupedSession records the settings it was opened with and echoes.
type groupedSession struct{}

func (groupedSession) Call(adapter.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"text":"x"}`), nil
}
func (groupedSession) Close() error { return nil }

// Forge 07f's additions, on a grouped test type: its tenant connections
// live under tenant:1/connectors:grp, where a contributed role scoped
// there manages them and no others; a validator refuses what the schema
// cannot; a ref setting reaches the adapter as the item it names.
func TestGroupsRolesValidatorsRefs(t *testing.T) {
	f := setup(t)
	// The grouped type, a section it refers to, a validator and a role
	settings := `{"type":"object","properties":{
  "user":{"type":"string","title":"User"},
  "password":{"type":"secret","title":"Password"},
  "profile":{"type":"string","title":"Profile","ref":"test.profiles#profiles"}},"required":["user"]}`
	grouped := connectors.Type{ID: "test.grouped", Contracts: []string{connectorstest.ContractRef}, Source: connectors.SourceBuiltin, Group: "grp",
		Settings: json.RawMessage(settings), Meta: connectors.Meta{Title: "Grouped", Description: "A test type in the grp access group", Tags: []string{"test"}}}
	if p := connectors.CheckType(grouped, func(string) (connectors.Contract, bool) { return connectorstest.Contract(), true }); len(p) > 0 {
		t.Fatalf("CheckType: %v", p)
	}
	connectors.ContributeBuiltins(f.reg, "grouped", nil, []connectors.Type{grouped})
	config.Sections.Register(f.reg, config.Section{Descriptor: extensions.Descriptor{ID: "test.profiles"}, Title: "Profiles", Scopes: []string{"tenant"},
		Schema: json.RawMessage(`{"type":"object","properties":{"profiles":{"type":"array","key":"name","items":{"type":"object","properties":{"name":{"type":"string"},"host":{"type":"string"}}}}}}`)})
	connections.Validators.Register(f.reg, connections.Validator{Descriptor: extensions.Descriptor{ID: "test.grouped"}, Types: []string{"test.grouped"},
		Validate: func(_ features.ServiceArgs, c connections.Proposed) []connections.FieldProblem {
			if c.Owner != connections.OwnerTenant && c.Settings["profile"] != nil {
				return []connections.FieldProblem{{Path: "settings.profile", Message: "is the tenant's alone"}}
			}
			return nil
		}})
	authz.ContributedRoles.Register(f.reg, authz.ContributedRole{Descriptor: extensions.Descriptor{ID: "grp.admin"}, Role: authz.Role{
		ID: 100, Name: "Group administrator", Description: "Manages the tenant's grp connections",
		Permissions: []string{"connectors:grp/connection.read", "connectors:grp/connection.use", "connectors:grp/connection.manage"}}})
	pol := installPolicy(t, f)
	// User 4 is a member (Viewer) and the group's administrator; user 5
	// only a Viewer
	for _, g := range []authz.GrantArgs{{UserID: 4, RoleID: authz.RoleViewer, Resource: "tenant:1"}, {UserID: 4, RoleID: 100, Resource: "tenant:1"},
		{UserID: 5, RoleID: authz.RoleViewer, Resource: "tenant:1"}} {
		if _, err := pol.Grant(context.Background(), nil, g); err != nil {
			t.Fatal(err)
		}
	}
	f.live.Config, f.live.Extensions = profiles{`{"profiles":[{"name":"p1","host":"h1.example"}]}`}, f.reg
	f.deps = *f.live
	var mu sync.Mutex
	var opened json.RawMessage
	f.serve(adapter.Options{Name: "grouped", Types: []connectors.Type{grouped}, Contracts: []connectors.Contract{connectorstest.Contract()},
		Impls: []adapter.Impl{{Type: "test.grouped", Open: func(_ adapter.Context, conn adapter.Connection) (adapter.Session, error) {
			mu.Lock()
			opened = conn.Settings
			mu.Unlock()
			return groupedSession{}, nil
		}}}})
	f.online("test.grouped")
	make := func(as uint64, typ, owner string, s map[string]any) (connections.Connection, string) {
		res := f.doAs(user(as), "connection.create", "", map[string]any{"type": typ, "name": "C", "owner": owner, "settings": s, "secrets": map[string]any{"password": "p"}})
		if res.Common.Code >= 300 {
			return connections.Connection{}, res.Common.Message + " " + strings.TrimSpace(func() string { b, _ := json.Marshal(res.Problems); return string(b) }())
		}
		return f.connection(res.Target, res.Seq), ""
	}
	// The group's role makes a tenant grp connection, but no other kind
	c, why := make(4, "test.grouped", "tenant", map[string]any{"user": "u", "profile": "p1"})
	if why != "" {
		t.Fatalf("the group's administrator: %s", why)
	}
	if c.Group != "grp" {
		t.Errorf("group %q", c.Group)
	}
	if _, why := make(4, "test.secure", "tenant", map[string]any{"user": "u"}); why == "" {
		t.Errorf("the group's administrator made a tenant connection of another type")
	}
	if _, why := make(2, "test.grouped", "tenant", map[string]any{"user": "u"}); why == "" {
		t.Errorf("an Editor made a tenant grp connection")
	}
	if _, why := make(5, "test.grouped", "tenant", map[string]any{"user": "u"}); why == "" {
		t.Errorf("a Viewer made a tenant grp connection")
	}
	if _, why := make(3, "test.grouped", "tenant", map[string]any{"user": "u"}); why != "" {
		t.Errorf("an Administrator could not make a tenant grp connection: %s", why)
	}
	// It may change its own group's connections, and read and use them
	if res := f.doAs(user(4), "connection.update", c.ID, map[string]any{"settings": map[string]any{"user": "v"}}); res.Common.Code >= 300 {
		t.Errorf("the group's administrator could not update: %s %+v", res.Common.Message, res.Problems)
	}
	// The validator refuses a personal connection naming a profile
	if _, why := make(2, "test.grouped", "me", map[string]any{"user": "u", "profile": "p1"}); !strings.Contains(why, "settings.profile") {
		t.Errorf("the validator did not refuse: %q", why)
	}
	if _, why := make(2, "test.grouped", "me", map[string]any{"user": "u"}); why != "" {
		t.Errorf("a personal connection without a profile: %s", why)
	}
	// The adapter gets the profile's item, the secret in place
	if _, e := f.deps.Dispatcher.Call(user(2), connectors.Call{Contract: connectorstest.ContractRef, Operation: "echo", Connection: c.ID, Args: json.RawMessage(`{"text":"x"}`)}); e != nil {
		t.Fatalf("call: %v", e)
	}
	mu.Lock()
	var got map[string]any
	_ = json.Unmarshal(opened, &got)
	mu.Unlock()
	if p, _ := got["profile"].(map[string]any); p["host"] != "h1.example" || got["password"] != "p" || got["user"] != "v" {
		t.Errorf("resolved settings: %s", opened)
	}
	// A profile that is gone makes resolving fail
	f.deps.Config = profiles{`{"profiles":[]}`} // a copy: Settings is given it
	if _, err := connections.Settings(context.Background(), f.deps, 1, c); err == nil || !strings.Contains(err.Error(), "not in test.profiles") {
		t.Errorf("a missing profile: %v", err)
	}
}
