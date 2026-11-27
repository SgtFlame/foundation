package infra_test

import (
	"context"
	"encoding/json"
	"github.com/SgtFlame/foundation/backend/features"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// TestEveryOpUndoes runs each infra.config op, then its logged inverse,
// and expects the draft as it was before: what Foundation's
// apps/ops TestEveryOpUndoes does for the other families, here because
// infra ops are refused outside the root tenant.
func TestEveryOpUndoes(t *testing.T) {
	st := newStack(t)
	ops.Contribute(st.reg)
	steps := []struct {
		typ  string
		args m
	}{
		{"infra.config.put", m{"kind": "accounts", "item": m{"id": "test", "name": "Test", "provider": "infratest"}}},
		{"infra.config.put", m{"kind": "sites", "item": m{"id": "west", "name": "West", "account": "test", "region": "r1"}}},
		{"infra.config.put", m{"kind": "sites", "item": m{"id": "west", "name": "West coast", "account": "test", "region": "r2"}}},
		{"infra.config.put", m{"kind": "hostPools", "item": m{"id": "general", "site": "west", "size": "m", "count": 2}}},
		{"infra.config.settings", m{"onDrift": "correct"}},
		{"infra.config.remove", m{"kind": "hostPools", "id": "general"}},
		{"infra.config.import", m{"document": m{"sites": []m{{"id": "lab", "name": "Lab", "onPremise": true}}}, "force": true}},
	}
	draft := func() string {
		raw, _ := json.Marshal(st.config().Draft.Normalized())
		return string(raw)
	}
	seen := map[string]bool{}
	for _, s := range steps {
		before := draft()
		st.must(s.typ, s.args)
		after := draft()
		if err := st.p.Settle(context.Background(), features.RootTenantID, ops.Projector); err != nil {
			t.Fatal(err)
		}
		entries, err := ops.Entries(operator(7), st.p, ops.NoApp)
		if err != nil {
			t.Fatal(err)
		}
		last := entries[len(entries)-1]
		if last.Command != s.typ || len(last.Inverse) == 0 {
			t.Fatalf("%s: last op = %+v", s.typ, last)
		}
		for _, op := range last.Inverse {
			st.must(op.Command, json.RawMessage(op.Args))
		}
		if got := draft(); got != before {
			t.Fatalf("%s undone:\n got %s\nwant %s", s.typ, got, before)
		}
		st.must(s.typ, s.args) // again, so the next step builds on it
		if draft() != after {
			t.Fatalf("%s redone differs:\n got %s\nwant %s", s.typ, draft(), after)
		}
		seen[s.typ] = true
	}
	for _, c := range []string{"infra.config.put", "infra.config.remove", "infra.config.settings", "infra.config.import"} {
		if !seen[c] {
			t.Errorf("no step runs %s", c)
		}
	}
	_ = infra.DefaultConfig
}
