package connectors_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice"
	cximpl "github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/impl"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice/server"
	"github.com/SgtFlame/foundation/backend/features/services"
)

// remoteBooks serves the connectors' data service on the fixture's NATS
// and returns its books as an adapter in another process holds them.
func (f *fixture) remoteBooks() connectors.Books {
	f.t.Helper()
	srv, err := server.Serve(f.srv.Conn(), cximpl.New(cximpl.WithJetStream(f.srv.JS())), services.TrustHeaders())
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(srv.Stop)
	nc, err := f.srv.Connect("adapter-books")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(nc.Close)
	return connectorsservice.Remote(proxy.New(&services.Client{NC: nc, Caller: "adapter"}))
}

// An adapter whose books are svc.connectors announces itself and serves
// calls with no JetStream handle (forge 21e-1).
func TestAdapterOnTheConnectorsService(t *testing.T) {
	f := setup(t, nil)
	o := connectorstest.Options("remote", nil)
	o.Books = f.remoteBooks()
	f.serve(o)
	f.online("test.echo", true, 2*time.Second)
	if out, e := f.echo("hi"); e != nil || out["by"] != "remote" {
		t.Fatalf("echo: %v %v", out, e)
	}
}

// The books behave the same through the service: leases are taken once
// and lost when they move on, a lock waits for its holder, cursors and
// sources round-trip, and an event is published once by its id.
func TestBooksThroughTheService(t *testing.T) {
	f := setup(t, nil)
	b := f.remoteBooks()
	ctx := context.Background()

	key := connectors.SourceKey(1, "c1")
	rev, err := b.LeaseCreate(ctx, key, connectors.Lease{Adapter: "a", Instance: "i1"})
	if err != nil || rev == 0 {
		t.Fatalf("lease: %d %v", rev, err)
	}
	if _, err := b.LeaseCreate(ctx, key, connectors.Lease{Adapter: "a", Instance: "i2"}); !errors.Is(err, connectors.ErrLeaseTaken) {
		t.Fatalf("a held lease taken again: %v", err)
	}
	next, err := b.LeaseUpdate(ctx, key, connectors.Lease{Adapter: "a", Instance: "i1"}, rev)
	if err != nil || next <= rev {
		t.Fatalf("refresh: %d %v", next, err)
	}
	if _, err := b.LeaseUpdate(ctx, key, connectors.Lease{Adapter: "a", Instance: "i1"}, rev); !errors.Is(err, connectors.ErrLeaseLost) {
		t.Fatalf("a stale refresh: %v", err)
	}
	if err := b.LeaseDelete(ctx, key, next); err != nil {
		t.Fatal(err)
	}

	release, err := b.Lock(ctx, 1, "c1", "i1")
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	if _, err := b.Lock(short, 1, "c1", "i2"); err == nil {
		t.Fatal("a held lock taken again")
	}
	cancel()
	go func() { time.Sleep(200 * time.Millisecond); release() }()
	again, err := b.Lock(ctx, 1, "c1", "i2")
	if err != nil {
		t.Fatalf("after its release: %v", err)
	}
	again()

	if _, ok, err := b.Cursor(ctx, key); ok || err != nil {
		t.Fatalf("no cursor yet: %v %v", ok, err)
	}
	if err := b.PutCursor(ctx, key, connectors.Cursor{Cursor: json.RawMessage(`{"n":3}`), At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if c, ok, err := b.Cursor(ctx, key); !ok || err != nil || string(c.Cursor) != `{"n":3}` {
		t.Fatalf("cursor %+v %v %v", c, ok, err)
	}

	wctx, stop := context.WithCancel(ctx)
	defer stop()
	updates, err := b.Sources(wctx)
	if err != nil {
		t.Fatal(err)
	}
	kv, err := connectors.SourcesKV(ctx, f.srv.JS())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(connectors.Source{Tenant: 1, Connection: "c1", Type: "test.echo"})
	if _, err := kv.Put(ctx, key, raw); err != nil {
		t.Fatal(err)
	}
	for seen := false; !seen; {
		select {
		case u, ok := <-updates:
			if !ok {
				t.Fatal("the watch ended")
			}
			seen = u.Key == key && u.Source != nil && u.Source.Connection == "c1"
		case <-time.After(3 * time.Second):
			t.Fatal("no source update")
		}
	}

	for i := 0; i < 2; i++ {
		if err := b.Publish(ctx, 1, "c1", connectorstest.ContractRef, "ping", "e1", json.RawMessage(`{"n":1}`), time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := connectors.EventStream(ctx, f.srv.JS(), 1)
	if err != nil {
		t.Fatal(err)
	}
	info, err := st.Info(ctx)
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("events kept: %+v %v", info, err)
	}
	if err := b.Publish(ctx, 1, "c1", "test.echo", "ping", "e2", nil, time.Time{}); err == nil {
		t.Fatal("an event naming no contract version was taken")
	}
}
