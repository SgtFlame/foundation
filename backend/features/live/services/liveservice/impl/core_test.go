package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice/server"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/services"
)

type count struct {
	Value int `json:"value"`
}

// core is the live core served on a test NATS server, and a proxy of it
// as a process without a JetStream handle would hold.
func core(t *testing.T) liveservice.Service {
	t.Helper()
	reg := extensions.NewRegistry()
	area := live.AppArea("test")
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "test.counter"}, Title: "Counters", Area: area,
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var c count
			if f.State != nil {
				_ = json.Unmarshal(f.State, &c)
			}
			c.Value++
			return json.Marshal(c)
		},
	})
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "test.increment"}, Title: "Increment", Area: area, TargetType: "counter",
		Projection: "test.counter", Action: "test.write", Events: []string{"incremented"},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if string(d.Args) == `{"refuse":true}` {
				return nil, []live.Problem{{Path: "args.refuse", Message: "refused"}}
			}
			return []live.NewEvent{{Type: "incremented", Data: map[string]int{"by": 1}}}, nil
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "test.count"}, Title: "Count",
		Run: live.ValueOf("test.counter", func(live.Feed) (string, error) { return "counter.main", nil }, json.RawMessage(`{"value":0}`)),
	})
	ns := natsd.StartTest(t)
	p := project.New(ns.JS(), reg)
	t.Cleanup(p.Close)
	real := New(WithBus(bus.New(p)), WithSubscriptions(subscribe.New(p, reg)), WithStore(p), WithRegistry(reg))
	srv, err := server.Serve(ns.Conn(), real, services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return proxy.New(&services.Client{NC: ns.Conn(), Caller: "test"})
}

func user() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

// A process without the JetStream handle sends commands, reads and
// subscribes through svc.live (forge 21d-4).
func TestTheLiveCoreThroughTheProxy(t *testing.T) {
	c := core(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := user()
	sub.Ctx = ctx
	req := c.Subscribe(liveservice.SubscribeArgs{ServiceArgs: sub, Query: "test.count"})
	next := func() liveservice.SubscribeEvent {
		t.Helper()
		select {
		case e, ok := <-req.Events:
			if !ok {
				t.Fatal("the subscription ended")
			}
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		return liveservice.SubscribeEvent{}
	}
	if e := next(); !e.Snapshot || string(e.Data) != `{"value":0}` {
		if e.Failed != nil {
			t.Fatalf("first event failed: %+v", *e.Failed)
		}
		t.Fatalf("first event %+v", e)
	}

	cmd := liveservice.Command{ID: "c1", Type: "test.increment", Target: "main"}
	r := c.Execute(liveservice.ExecuteArgs{ServiceArgs: user(), Command: cmd})
	if r.Common.Code != http.StatusOK || r.Seq == 0 || r.Duplicate {
		t.Fatalf("execute %+v", r)
	}
	// A retry of the same command is a duplicate, not a second increment.
	again := c.Execute(liveservice.ExecuteArgs{ServiceArgs: user(), Command: cmd})
	if again.Common.Code != http.StatusOK || !again.Duplicate {
		t.Fatalf("retry %+v", again)
	}
	// A refusal keeps its problems across the hop.
	bad := c.Execute(liveservice.ExecuteArgs{ServiceArgs: user(), Command: liveservice.Command{ID: "c2", Type: "test.increment", Target: "main", Args: json.RawMessage(`{"refuse":true}`)}})
	if bad.Common.Code == http.StatusOK || len(bad.Problems) != 1 || bad.Problems[0].Path != "args.refuse" {
		t.Fatalf("refusal %+v", bad)
	}

	for {
		e := next()
		if e.Failed != nil {
			t.Fatalf("failed %+v", e.Failed)
		}
		if e.Delta != nil && e.Delta.Op == "replace" && string(e.Delta.Value) == `{"value":1}` {
			break
		}
	}

	tx := c.Transact(liveservice.TransactArgs{ServiceArgs: user(), Command: liveservice.Command{ID: "t1"},
		Ops: []live.Op{{Command: "test.increment", Target: "main"}, {Command: "test.increment", Target: "main"}}})
	if tx.Common.Code != http.StatusOK || len(tx.Ops) != 2 {
		t.Fatalf("transact %+v", tx)
	}

	svc := features.ServiceArgs{Ctx: context.Background(), Principal: features.ServicePrincipalFor(features.LocalPrincipal().TenantID, "test")}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rd := c.Read(liveservice.ReadArgs{ServiceArgs: svc, Projector: "test.counter", Key: "counter.main"})
		if rd.Common.Code != http.StatusOK {
			t.Fatalf("read %+v", rd)
		}
		if rd.Found && string(rd.Value.Value) == `{"value":3}` {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("read %+v", rd)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Reading a projection directly is for services only.
	if rd := c.Read(liveservice.ReadArgs{ServiceArgs: user(), Projector: "test.counter", Key: "counter.main"}); rd.Common.Code != http.StatusForbidden {
		t.Fatalf("a user's read %+v", rd.Common)
	}
}
