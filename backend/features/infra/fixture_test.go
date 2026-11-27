package infra_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
)

type stack struct {
	t   *testing.T
	bus *bus.Bus
	p   *project.Projections
	reg *extensions.Registry
	n   int
}

func newStack(t *testing.T) *stack {
	reg := extensions.NewRegistry()
	var deps infra.Deps
	infra.Contribute(reg, func() infra.Deps { return deps })
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	return &stack{t: t, bus: b, p: p, reg: reg}
}

func operator(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: features.RootTenantID}}
}

func (st *stack) sendAs(args features.ServiceArgs, typ, target string, a any) bus.Result {
	st.t.Helper()
	st.n++
	raw, _ := json.Marshal(a)
	return st.bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("t%d-%s", st.n, typ), Type: typ, Target: target, Args: raw})
}

func (st *stack) send(typ string, a any) bus.Result {
	st.t.Helper()
	return st.sendAs(operator(7), typ, infra.DefaultConfig, a)
}

func (st *stack) must(typ string, a any) bus.Result {
	st.t.Helper()
	r := st.send(typ, a)
	if r.Common.Code >= 300 {
		st.t.Fatalf("%s: %d %s %+v", typ, r.Common.Code, r.Common.Message, r.Problems)
	}
	return r
}

func (st *stack) get(projector, key string, into any) bool {
	st.t.Helper()
	ctx := context.Background()
	if err := st.p.Settle(ctx, features.RootTenantID, projector); err != nil {
		st.t.Fatal(err)
	}
	v, ok, err := st.p.Get(ctx, features.RootTenantID, projector, key)
	if err != nil {
		st.t.Fatal(err)
	}
	if ok && into != nil {
		if err := json.Unmarshal(v.Value, into); err != nil {
			st.t.Fatal(err)
		}
	}
	return ok
}

func (st *stack) config() infra.Config {
	st.t.Helper()
	var c infra.Config
	st.get(infra.ConfigProjector, infra.ConfigType+"."+infra.DefaultConfig, &c)
	return c
}

type m = map[string]any
