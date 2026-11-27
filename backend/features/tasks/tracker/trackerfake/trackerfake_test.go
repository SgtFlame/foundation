package trackerfake_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackerfake"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker/trackertest"
)

func ctxFor(contract, op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: contract, Operation: op, Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: 1}}}
}

func open(t *testing.T, s *trackerfake.Server) adapter.Session {
	t.Helper()
	sess, err := s.Impl().Open(ctxFor(tracker.Ref, ""), adapter.Connection{ID: "c1", Type: trackerfake.TypeID, Tenant: 1})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func caller(sess adapter.Session, contract string) trackertest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return sess.Call(ctxFor(contract, op), op, raw)
	}
}

// watch runs the session's Watch, collecting its events.
func watch(t *testing.T, sess adapter.Session) func() []adapter.Event {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var events []adapter.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		c := ctxFor(tracker.Ref, "")
		c.Context = ctx
		_ = sess.(adapter.Watcher).Watch(c, nil, func(e adapter.Event) error {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	return func() []adapter.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]adapter.Event(nil), events...)
	}
}

func TestContractSuite(t *testing.T) {
	sess := open(t, trackerfake.New())
	control := caller(sess, trackerfake.ControlRef)
	if _, err := control("seed", map[string]any{"board": map[string]any{"id": "b1", "name": "Website relaunch", "lists": []string{"To do", "Doing", "Done"}, "members": []string{"Ada Lovelace"}}, "count": 10}); err != nil {
		t.Fatal(err)
	}
	trackertest.Run(t, caller(sess, tracker.Ref), trackertest.Options{Board: "b1", Events: watch(t, sess)})
}

func TestTypeAndControlPassTheChecks(t *testing.T) {
	if ps := connectors.CheckContract(trackerfake.ControlContract()); len(ps) > 0 {
		t.Fatal(ps)
	}
	if ps := connectors.CheckType(trackerfake.Type(), func(ref string) (connectors.Contract, bool) {
		switch ref {
		case tracker.Ref:
			return tracker.Contract(), true
		case trackerfake.ControlRef:
			return trackerfake.ControlContract(), true
		}
		return connectors.Contract{}, false
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}

// TestSeedChangeAndFaults seeds the acceptance test's 500 cards, changes
// one outside (an event with its new version), and fails calls.
func TestSeedChangeAndFaults(t *testing.T) {
	sess := open(t, trackerfake.New())
	trk, control := caller(sess, tracker.Ref), caller(sess, trackerfake.ControlRef)
	events := watch(t, sess)
	raw, err := control("seed", map[string]any{"board": map[string]any{"id": "b1", "lists": []string{"To do", "Doing", "Review", "Done"}}, "count": 500})
	if err != nil || string(raw) != `{"board":"b1","cards":500}` {
		t.Fatalf("seed = %s, %v", raw, err)
	}
	n, cursor := 0, ""
	for {
		var page struct {
			Cards  []tracker.Card
			Cursor string
		}
		raw, err := trk("card.list", map[string]any{"board": "b1", "limit": 120, "cursor": cursor})
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &page)
		n += len(page.Cards)
		if page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	if n != 500 {
		t.Fatalf("listed %d cards", n)
	}
	var before tracker.Card
	raw, _ = trk("card.get", map[string]any{"id": "c7"})
	_ = json.Unmarshal(raw, &before)
	raw, err = control("change", map[string]any{"card": "c7", "set": map[string]any{"title": "Renamed in the tracker", "list": "b1-l2"}, "comment": "Moved it"})
	if err != nil {
		t.Fatal(err)
	}
	var changed struct{ Version string }
	_ = json.Unmarshal(raw, &changed)
	if changed.Version == before.Version {
		t.Fatal("an outside change kept the version")
	}
	// The sync's update against the old version is a conflict
	_, err = trk("card.update", map[string]any{"id": "c7", "version": before.Version, "set": map[string]any{"title": "Ours"}})
	var ce *connectors.Error
	if !errors.As(err, &ce) || ce.Code != connectors.CodeConflict {
		t.Fatalf("stale update = %v", err)
	}
	if _, err := control("change", map[string]any{"card": "c7", "set": map[string]any{"colour": "red"}}); err == nil {
		t.Fatal("a change of no field passed")
	}
	// Seeding raised nothing; the change raised card.changed (moved) and
	// comment.created, the last with the card's version
	var got []string
	for range 200 {
		got = nil
		for _, e := range events() {
			var p struct{ Change, Version string }
			_ = json.Unmarshal(e.Payload, &p)
			got = append(got, e.Name+":"+p.Change+":"+p.Version)
		}
		if len(got) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "card.changed:moved:") || got[1] != "comment.created::"+changed.Version {
		t.Fatalf("events = %v", got)
	}

	if _, err := control("fail", map[string]any{"operation": "card.update", "times": 2, "kind": "rateLimited"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := trk("card.update", map[string]any{"id": "c8", "set": map[string]any{"title": "x"}})
		if !errors.As(err, &ce) || ce.Code != connectors.CodeRateLimited || !ce.Retryable() {
			t.Fatalf("call %d = %v", i, err)
		}
	}
	if _, err := trk("card.update", map[string]any{"id": "c8", "set": map[string]any{"title": "x"}}); err != nil {
		t.Fatalf("third call = %v", err)
	}
	if _, err := control("fail", map[string]any{"operation": "card.explode"}); err == nil {
		t.Fatal("a fault on no operation was taken")
	}
	var state struct {
		Boards []struct {
			ID    string
			Cards int
		}
		Events int
		Faults []any
	}
	raw, _ = control("state", map[string]any{})
	_ = json.Unmarshal(raw, &state)
	if len(state.Boards) != 1 || state.Boards[0].Cards != 500 || state.Events != 3 || len(state.Faults) != 0 {
		t.Fatalf("state = %s", raw)
	}
}

// fakeDispatcher calls the session directly, as the dispatcher would
// through an adapter.
type fakeDispatcher struct{ sess adapter.Session }

func (d fakeDispatcher) Call(args features.ServiceArgs, c connectors.Call) (json.RawMessage, *connectors.Error) {
	res, err := d.sess.Call(ctxFor(c.Contract, c.Operation), c.Operation, c.Args)
	if err != nil {
		return nil, connectors.AsError(err)
	}
	return res, nil
}

func TestControlCall(t *testing.T) {
	sess := open(t, trackerfake.New())
	reg := extensions.NewRegistry()
	reg.Declare(connectors.Contracts, connectors.Types, live.Calls)
	trackerfake.Contribute(reg, func() trackerfake.Dispatcher { return fakeDispatcher{sess} })
	var call live.Call
	for _, c := range live.Calls.All(reg) {
		if c.ID == trackerfake.ControlCall {
			call = c
		}
	}
	if call.Run == nil {
		t.Fatal("the control call is not registered")
	}
	var emitted []json.RawMessage
	run := live.CallRun{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()},
		Args: json.RawMessage(`{"connection":"c1","operation":"seed","args":{"board":{"id":"b9"},"count":3}}`),
		Emit: func(name string, data any) { raw, _ := json.Marshal(data); emitted = append(emitted, raw) }}
	if fail := call.Run(run); fail != nil {
		t.Fatalf("seed failed: %+v", fail)
	}
	if len(emitted) != 1 || string(emitted[0]) != `{"board":"b9","cards":3}` {
		t.Fatalf("emitted %s", emitted)
	}
	run.Args = json.RawMessage(`{"connection":"c1","operation":"change","args":{"card":"nope"}}`)
	if fail := call.Run(run); fail == nil || fail.Code != 404 {
		t.Fatalf("a change of no card = %+v", fail)
	}
}
