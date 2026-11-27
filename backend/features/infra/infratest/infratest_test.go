package infratest_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/contracttest"
	"github.com/SgtFlame/foundation/backend/features/infra/infratest"
)

func open(t *testing.T, s *infratest.Server, settings string) (adapter.Session, func(contract string) adapter.Context) {
	t.Helper()
	ctx := func(contract string) adapter.Context {
		return adapter.Context{Context: context.Background(), Contract: contract, Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: features.RootTenantID}}}
	}
	sess, err := s.Impl().Open(ctx(infra.ComputeRef), adapter.Connection{ID: "c1", Type: infratest.TypeID, Tenant: features.RootTenantID, Settings: json.RawMessage(settings)})
	if err != nil {
		t.Fatal(err)
	}
	return sess, ctx
}

func caller(sess adapter.Session, ctx func(string) adapter.Context, contract string) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		c := ctx(contract)
		c.Operation = op
		return sess.Call(c, op, raw)
	}
}

func TestContractSuite(t *testing.T) {
	sess, ctx := open(t, infratest.New(), `{"delayMs":30}`)
	contracttest.Run(t, caller(sess, ctx, infra.ComputeRef), contracttest.Options{Size: "m", Region: "r1"})
}

func TestTypeAndControlPassTheChecks(t *testing.T) {
	if ps := connectors.CheckContract(infratest.ControlContract()); len(ps) > 0 {
		t.Fatal(ps)
	}
	if ps := connectors.CheckType(infratest.Type(), func(ref string) (connectors.Contract, bool) {
		switch ref {
		case infra.ComputeRef:
			return infra.ComputeContract(), true
		case infratest.ControlRef:
			return infratest.ControlContract(), true
		}
		return connectors.Contract{}, false
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}

func TestFaultsAndVanish(t *testing.T) {
	sess, ctx := open(t, infratest.New(), `{}`)
	compute, control := caller(sess, ctx, infra.ComputeRef), caller(sess, ctx, infratest.ControlRef)
	if _, err := control("fail", map[string]any{"operation": "host.create", "times": 2}); err != nil {
		t.Fatal(err)
	}
	create := map[string]any{"id": "h1", "size": "m", "region": "r1"}
	for i := 0; i < 2; i++ {
		_, err := compute("host.create", create)
		var ce *connectors.Error
		if !errors.As(err, &ce) || ce.Code != connectors.CodeUnavailable || !ce.Retryable() {
			t.Fatalf("call %d = %v", i, err)
		}
	}
	if _, err := compute("host.create", create); err != nil {
		t.Fatalf("third call = %v", err)
	}
	// nested needs a size that runs VMs
	if _, err := compute("host.create", map[string]any{"id": "h2", "size": "m", "region": "r1", "nested": true}); err == nil {
		t.Fatal("a nested host on a size without vms was made")
	}
	if _, err := control("vanish", map[string]any{"host": "h1"}); err != nil {
		t.Fatal(err)
	}
	var list struct{ Hosts []infra.ProviderHost }
	raw, _ := compute("host.list", map[string]any{})
	_ = json.Unmarshal(raw, &list)
	if len(list.Hosts) != 0 {
		t.Fatalf("a vanished host is listed: %+v", list.Hosts)
	}
	var state struct {
		Hosts  []struct{ ID, State string }
		Faults []any
	}
	raw, _ = control("state", map[string]any{})
	_ = json.Unmarshal(raw, &state)
	if len(state.Hosts) != 1 || state.Hosts[0].State != infra.HostGone || len(state.Faults) != 0 {
		t.Fatalf("state = %s", raw)
	}
}

func TestWatchReportsChanges(t *testing.T) {
	sess, ctx := open(t, infratest.New(), `{"delayMs":20}`)
	compute := caller(sess, ctx, infra.ComputeRef)
	watcher := sess.(adapter.Watcher)
	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var states []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		c := ctx(infra.ComputeRef)
		c.Context = wctx
		_ = watcher.Watch(c, nil, func(e adapter.Event) error {
			var p struct{ Host, State string }
			_ = json.Unmarshal(e.Payload, &p)
			mu.Lock()
			states = append(states, p.Host+":"+p.State)
			mu.Unlock()
			return nil
		})
	}()
	if _, err := compute("host.create", map[string]any{"id": "h1", "size": "s", "region": "r2"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(states)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(states) < 2 || states[0] != "h1:creating" || states[1] != "h1:running" {
		t.Fatalf("events = %v", states)
	}
}
