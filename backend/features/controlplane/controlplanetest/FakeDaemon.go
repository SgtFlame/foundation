// Package controlplanetest holds the fake daemon (forge 20b-2): it answers
// control requests and writes facts and heartbeats as told, for tests of
// everything above the daemons and for Forge's `forge ctl fake`.
package controlplanetest

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/nats-io/nats.go"
)

// FakeDaemon plays a host's or an instance's daemon.
type FakeDaemon struct {
	C      *ctl.Control
	Target ctl.Target
	// Fail makes every request end failed with this message.
	Fail string

	mu       sync.Mutex
	sub      *nats.Subscription
	stop     chan struct{}
	requests []ctl.Request
}

// Start answers the target's requests: each is accepted at once, then
// reported accepted and done (or failed) on CTL.
func (f *FakeDaemon) Start(ctx context.Context) error {
	sub, err := f.C.NC.Subscribe(f.Target.Requests(), func(m *nats.Msg) {
		var r ctl.Request
		if err := json.Unmarshal(m.Data, &r); err != nil {
			raw, _ := json.Marshal(ctl.Reply{Error: "bad request"})
			_ = m.Respond(raw)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, r)
		fail := f.Fail
		f.mu.Unlock()
		raw, _ := json.Marshal(ctl.Reply{Accepted: true})
		_ = m.Respond(raw)
		_ = f.C.Report(ctx, ctl.Event{Request: r.ID, Kind: ctl.EventAccepted, Target: f.Target, Verb: r.Verb})
		end := ctl.Event{Request: r.ID, Kind: ctl.EventDone, Target: f.Target, Verb: r.Verb, Message: r.Verb + " done"}
		if fail != "" {
			end.Kind, end.Message = ctl.EventFailed, fail
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
func (f *FakeDaemon) Requests() []ctl.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ctl.Request(nil), f.requests...)
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
	_ = f.C.Beat(ctx, f.Target, ctl.Health{})
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
				_ = f.C.Beat(ctx, f.Target, ctl.Health{})
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

// FakeAgent plays an instance agent for rollouts (forge 20f-2): it beats,
// and installs what its desired state's pins and units name by reporting
// facts {packages, units} as the agent does, its units active unless
// Unhealthy. Hold keeps it from installing anything.
type FakeAgent struct {
	C         *ctl.Control
	Target    ctl.Target
	Unhealthy atomic.Bool
	Hold      atomic.Bool

	mu        sync.Mutex
	installed map[string]string
}

// Start runs it until ctx ends.
func (f *FakeAgent) Start(ctx context.Context) {
	d := &FakeDaemon{C: f.C, Target: f.Target}
	d.Beat(ctx, ctl.HealthTTL/4)
	f.mu.Lock()
	f.installed = map[string]string{}
	f.mu.Unlock()
	go func() {
		defer d.StopBeating()
		var last string
		for ctx.Err() == nil {
			if !f.Hold.Load() {
				raw, ok, _ := f.C.Desired(ctx, f.Target)
				var doc struct {
					Pins  []struct{ Name, Version string }          `json:"pins"`
					Units []struct{ Name, Package, Version string } `json:"units"`
				}
				if ok && json.Unmarshal(raw, &doc) == nil {
					f.mu.Lock()
					for _, p := range doc.Pins {
						f.installed[p.Name] = p.Version
					}
					for _, u := range doc.Units {
						if u.Package != "" {
							f.installed[u.Package] = u.Version
						}
					}
					state := "active"
					if f.Unhealthy.Load() {
						state = "failed"
					}
					out, _ := json.Marshal(map[string]any{"packages": f.installed, "units": map[string]string{"axiom-serve@web": state}})
					f.mu.Unlock()
					if string(out) != last {
						if f.C.PutFacts(ctx, f.Target, json.RawMessage(out)) == nil {
							last = string(out)
						}
					}
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
}

// Installed is the version of a package it reports.
func (f *FakeAgent) Installed(pkg string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installed[pkg]
}
