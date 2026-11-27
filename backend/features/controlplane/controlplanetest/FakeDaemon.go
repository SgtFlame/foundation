// Package controlplanetest holds the fake daemon (forge 20b-2): it answers
// control requests and writes facts and heartbeats as told, for tests of
// everything above the daemons and for Forge's `forge ctl fake`.
package controlplanetest

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/nats-io/nats.go"
)

// FakeDaemon plays a host's or an instance's daemon.
type FakeDaemon struct {
	C      *controlplane.Control
	Target controlplane.Target
	// Fail makes every request end failed with this message.
	Fail string

	mu       sync.Mutex
	sub      *nats.Subscription
	stop     chan struct{}
	requests []controlplane.Request
}

// Start answers the target's requests: each is accepted at once, then
// reported accepted and done (or failed) on CTL.
func (f *FakeDaemon) Start(ctx context.Context) error {
	sub, err := f.C.NC.Subscribe(f.Target.Requests(), func(m *nats.Msg) {
		var r controlplane.Request
		if err := json.Unmarshal(m.Data, &r); err != nil {
			raw, _ := json.Marshal(controlplane.Reply{Error: "bad request"})
			_ = m.Respond(raw)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, r)
		fail := f.Fail
		f.mu.Unlock()
		raw, _ := json.Marshal(controlplane.Reply{Accepted: true})
		_ = m.Respond(raw)
		_ = f.C.Report(ctx, controlplane.Event{Request: r.ID, Kind: controlplane.EventAccepted, Target: f.Target, Verb: r.Verb})
		end := controlplane.Event{Request: r.ID, Kind: controlplane.EventDone, Target: f.Target, Verb: r.Verb, Message: r.Verb + " done"}
		if fail != "" {
			end.Kind, end.Message = controlplane.EventFailed, fail
		}
		_ = f.C.Report(ctx, end)
	})
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sub = sub
	f.mu.Unlock()
	return f.C.NC.Flush()
}

// Requests are the requests it has had.
func (f *FakeDaemon) Requests() []controlplane.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]controlplane.Request(nil), f.requests...)
}

// SetFacts writes its facts.
func (f *FakeDaemon) SetFacts(ctx context.Context, facts any) error {
	return f.C.PutFacts(ctx, f.Target, facts)
}

// Beat starts heartbeats every interval until StopBeating.
func (f *FakeDaemon) Beat(ctx context.Context, every time.Duration) {
	f.mu.Lock()
	if f.stop != nil {
		f.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	f.stop = stop
	f.mu.Unlock()
	_ = f.C.Beat(ctx, f.Target, controlplane.Health{})
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				_ = f.C.Beat(ctx, f.Target, controlplane.Health{})
			}
		}
	}()
}

// StopBeating stops the heartbeats, as a daemon that died.
func (f *FakeDaemon) StopBeating() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stop != nil {
		close(f.stop)
		f.stop = nil
	}
}

// Stop stops answering and beating.
func (f *FakeDaemon) Stop() {
	f.StopBeating()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sub != nil {
		_ = f.sub.Unsubscribe()
		f.sub = nil
	}
}
