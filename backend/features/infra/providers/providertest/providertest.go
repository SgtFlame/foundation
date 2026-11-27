// Package providertest opens a provider type's session for its tests and
// runs the infra.compute@1 contract suite on it.
package providertest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/contracttest"
)

// Open opens a session of impl on a root tenant connection with settings.
func Open(t testing.TB, impl adapter.Impl, settings any) adapter.Session {
	t.Helper()
	raw, _ := json.Marshal(settings)
	s, err := impl.Open(adapter.Context{Context: context.Background()}, adapter.Connection{ID: "conn1", Type: impl.Type, Tenant: features.RootTenantID, Settings: raw})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Caller calls a session's infra.compute operations.
func Caller(s adapter.Session) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return s.Call(adapter.Context{Context: context.Background(), Contract: infra.ComputeRef, Operation: op}, op, raw)
	}
}

// CheckType fails the test when t does not pass connectors.CheckType.
func CheckType(tb testing.TB, t connectors.Type) {
	tb.Helper()
	ps := connectors.CheckType(t, func(ref string) (connectors.Contract, bool) {
		return infra.ComputeContract(), ref == infra.ComputeRef
	})
	if len(ps) > 0 {
		tb.Fatalf("problems: %+v", ps)
	}
}
