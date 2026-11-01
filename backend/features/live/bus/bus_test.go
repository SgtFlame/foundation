package bus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
)

type count struct {
	Value int `json:"value"`
}

func setup(t *testing.T) (*bus.Bus, *project.Projections) {
	reg := extensions.NewRegistry()
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "test.counter"}, Title: "Counters", Area: live.AppArea("test"),
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
		Descriptor: extensions.Descriptor{ID: "test.increment"}, Title: "Increment", Area: live.AppArea("test"), TargetType: "counter",
		Projection: "test.counter", Action: "test.write", Events: []string{"incremented"},
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			return []live.NewEvent{{Type: "incremented", Data: map[string]int{"by": 1}}}, nil
		},
	})
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	return bus.New(p), p
}

func request(id string) bus.Request {
	return bus.Request{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, ID: id, Type: "test.increment", Target: "main"}
}

func TestConcurrentCommandsAllApply(t *testing.T) {
	b, p := setup(t)
	var wg sync.WaitGroup
	var last uint64
	var mu sync.Mutex
	for conn := 0; conn < 2; conn++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var inner sync.WaitGroup
			for i := 0; i < 50; i++ {
				inner.Add(1)
				go func() {
					defer inner.Done()
					res := b.Execute(request(fmt.Sprintf("c%d-%d", conn, i)))
					if res.Common.Code != 200 {
						t.Errorf("command failed: %+v", res)
						return
					}
					mu.Lock()
					last = max(last, res.Seq)
					mu.Unlock()
				}()
			}
			inner.Wait()
		}()
	}
	wg.Wait()
	v, err := p.WaitFor(context.Background(), features.LocalTenantID, "test.counter", "counter.main", last, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var c count
	_ = json.Unmarshal(v.Value, &c)
	if c.Value != 100 {
		t.Fatalf("counter is %d, want 100", c.Value)
	}
}

func TestResentCommandAppliesOnce(t *testing.T) {
	b, p := setup(t)
	first := b.Execute(request("same"))
	b.Execute(request("other"))
	again := b.Execute(request("same"))
	if first.Common.Code != 200 || again.Common.Code != 200 {
		t.Fatalf("failed: %+v %+v", first, again)
	}
	if !again.Duplicate || again.Seq != first.Seq {
		t.Fatalf("resent command was not dropped as a duplicate: first %+v, again %+v", first, again)
	}
	v, _ := p.WaitFor(context.Background(), features.LocalTenantID, "test.counter", "counter.main", 2, 5*time.Second)
	var c count
	_ = json.Unmarshal(v.Value, &c)
	if c.Value != 2 {
		t.Fatalf("counter is %d, want 2", c.Value)
	}
}

func TestUnknownCommand(t *testing.T) {
	b, _ := setup(t)
	r := request("x")
	r.Type = "nope"
	if res := b.Execute(r); res.Common.Code != 404 {
		t.Fatalf("got %+v", res)
	}
}
