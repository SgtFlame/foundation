package events_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/events"
	"github.com/SgtFlame/foundation/backend/features/events/durable"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go/jetstream"
)

type note struct{ Text string }

type seen struct {
	mu  sync.Mutex
	got []string
}

func (s *seen) add(n note) { s.mu.Lock(); s.got = append(s.got, n.Text); s.mu.Unlock() }
func (s *seen) wait(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		got := append([]string(nil), s.got...)
		s.mu.Unlock()
		if len(got) >= len(want) {
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
			if len(got) > len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %v, want %v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A bus over NATS delivers in the publishing process at once and in
// another process once, never twice where it was published.
func TestOver(t *testing.T) {
	ns := natsd.StartTest(t)
	a, b := events.NewBus[note](), events.NewBus[note]()
	for _, x := range []struct {
		bus  *events.Bus[note]
		name string
	}{{a, "a"}, {b, "b"}} {
		nc, err := ns.Connect(x.name)
		if err != nil {
			t.Fatal(err)
		}
		defer nc.Close()
		stop, err := events.Over(x.bus, nc, "test.notes")
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
	}
	var inA, inB seen
	a.Subscribe(inA.add)
	b.Subscribe(inB.add)
	a.Publish(note{"one"})
	if len(inA.got) != 1 {
		t.Fatalf("not delivered synchronously where published: %v", inA.got)
	}
	inB.wait(t, "one")
	b.Publish(note{"two"})
	inA.wait(t, "one", "two")
	inB.wait(t, "one", "two")
}

// A durable bus's follower gets every event in order, also those
// published while it was stopped; a process that only publishes
// delivers nothing itself.
func TestDurable(t *testing.T) {
	ns := natsd.StartTest(t)
	ctx := context.Background()
	js := func(name string) jetstream.JetStream {
		nc, err := ns.Connect(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatal(err)
		}
		return js
	}
	web, runner := events.NewBus[note](), events.NewBus[note]()
	stopWeb, err := durable.Link(ctx, js("web"), web, "test.files", "bus_test_files_index", false)
	if err != nil {
		t.Fatal(err)
	}
	defer stopWeb()
	var inWeb, inRunner seen
	web.Subscribe(inWeb.add)
	runner.Subscribe(inRunner.add)
	stopRunner, err := durable.Link(ctx, js("runner"), runner, "test.files", "bus_test_files_index", true)
	if err != nil {
		t.Fatal(err)
	}
	web.Publish(note{"a"})
	inRunner.wait(t, "a")
	stopRunner() // the runner restarts
	web.Publish(note{"b"})
	web.Publish(note{"c"})
	runner2 := events.NewBus[note]()
	var again seen
	runner2.Subscribe(again.add)
	stop2, err := durable.Link(ctx, js("runner2"), runner2, "test.files", "bus_test_files_index", true)
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	again.wait(t, "b", "c")
	if len(inWeb.got) != 0 {
		t.Fatalf("a publisher that does not follow delivered %v", inWeb.got)
	}
}
