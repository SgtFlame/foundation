package routes_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/services/routes"
	"github.com/nats-io/nats.go/jetstream"
)

type whoArgs struct {
	features.ServiceArgs
	N int
}

type whoResponse struct {
	Common features.ServiceResponse
	Server string
}

func who(name string, calls *atomic.Int32) map[string]services.Handler {
	return map[string]services.Handler{"Who": services.Unary(func(a whoArgs) whoResponse {
		calls.Add(1)
		return whoResponse{Common: features.OK(), Server: name}
	})}
}

func args(tenant uint64) whoArgs {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cancel
	return whoArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: tenant}}}
}

// A tenant routed to a dedicated server is served by it and every other
// tenant by the shared one; moving a tenant while calls run loses none.
func TestRouting(t *testing.T) {
	ns := natsd.StartTest(t)
	ctx := context.Background()
	js, err := jetstream.New(ns.Conn())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := services.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	table := services.NewRoutes()
	stop, err := routes.Watch(ctx, js, table)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	verify := services.VerifyWith(keys.Public())
	var sharedCalls, dedicatedCalls atomic.Int32
	shared, err := services.Serve(ns.Conn(), "who", who("shared", &sharedCalls), verify, services.WithRoutes(table))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Stop()
	dedicated, err := services.Serve(ns.Conn(), "who", who("t42", &dedicatedCalls), verify, services.WithRoutes(table), services.ForSuffix("t42"))
	if err != nil {
		t.Fatal(err)
	}
	defer dedicated.Stop()
	c := &services.Client{NC: ns.Conn(), Caller: "test", Keys: keys, Routes: table}
	call := func(tenant uint64) whoResponse {
		return services.Call[whoResponse](c, "who", "Who", args(tenant), services.Idempotent())
	}

	if r := call(42); r.Server != "shared" {
		t.Fatalf("unrouted tenant 42 served by %q (%+v)", r.Server, r.Common)
	}
	// Move tenant 42 while calls for it run
	var wg sync.WaitGroup
	var failed atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if r := call(42); r.Common.Code != http.StatusOK {
					failed.Add(1)
					t.Errorf("a call for the moving tenant failed: %+v", r.Common)
				}
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	if err := routes.Set(ctx, js, "who", 42, "t42"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for table.Suffix("who", 42) != "t42" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(42); r.Server != "t42" {
		t.Fatalf("routed tenant 42 served by %q", r.Server)
	}
	if r := call(7); r.Server != "shared" {
		t.Fatalf("tenant 7 served by %q", r.Server)
	}
	// A caller whose table is behind is refused by the shared server and
	// resent once its table catches up
	stale := services.NewRoutes()
	c2 := &services.Client{NC: ns.Conn(), Caller: "stale", Keys: keys, Routes: stale}
	go func() { time.Sleep(50 * time.Millisecond); stale.Set("who", 42, "t42") }()
	if r := services.Call[whoResponse](c2, "who", "Who", args(42)); r.Server != "t42" {
		t.Fatalf("a stale caller was served by %q (%+v)", r.Server, r.Common)
	}
	// Back to the shared servers
	if err := routes.Set(ctx, js, "who", 42, ""); err != nil {
		t.Fatal(err)
	}
	for table.Suffix("who", 42) != "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(42); r.Server != "shared" {
		t.Fatalf("unrouted again, tenant 42 served by %q", r.Server)
	}
}
