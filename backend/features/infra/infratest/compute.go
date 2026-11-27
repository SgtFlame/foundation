package infratest

import (
	"context"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// Compute is infra.Compute over an open session of the test server,
// called directly: for tests that do not run the connector bus.
type Compute struct{ Session adapter.Session }

// Call runs one infra.compute operation on the session, whatever the
// account.
func (c Compute) Call(args features.ServiceArgs, _ infra.Account, op string, in any) (json.RawMessage, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	ctx := args.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return c.Session.Call(adapter.Context{Context: ctx, Args: args, Contract: infra.ComputeRef, Operation: op}, op, raw)
}

// Open opens a session of a new test server for the root tenant's
// connection id, as infra.Compute.
func Open(ctx context.Context, connection string) (Compute, error) {
	args := features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalService, TenantID: features.RootTenantID}}
	s, err := New().Impl().Open(adapter.Context{Context: ctx, Args: args}, adapter.Connection{ID: connection, Type: TypeID, Tenant: features.RootTenantID})
	if err != nil {
		return Compute{}, err
	}
	return Compute{Session: s}, nil
}
