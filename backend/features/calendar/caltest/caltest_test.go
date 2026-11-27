package caltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/caltest"
	"github.com/SgtFlame/foundation/backend/features/calendar/contracttest"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
)

func ctxFor(contract string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: contract, Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: 1}}}
}

func open(t *testing.T, s *caltest.Server, conn, settings string) adapter.Session {
	t.Helper()
	sess, err := s.Impl().Open(ctxFor(calendar.EventsRef), adapter.Connection{ID: conn, Type: caltest.TypeID, Tenant: 1, Settings: json.RawMessage(settings)})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func caller(sess adapter.Session, contract string) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		c := ctxFor(contract)
		c.Operation = op
		return sess.Call(c, op, raw)
	}
}

func TestContractSuite(t *testing.T) {
	sess := open(t, caltest.New(), "c1", `{"email":"tony@example.org","name":"Tony","zone":"Europe/London"}`)
	control := caller(sess, caltest.ControlRef)
	contracttest.Run(t, caller(sess, calendar.EventsRef), contracttest.Options{Self: "tony@example.org", Expire: func() error {
		_, err := control("expireCursor", map[string]any{})
		return err
	}})
}

func TestContractsPassTheChecks(t *testing.T) {
	for _, c := range []connectors.Contract{calendar.EventsContract(), caltest.ControlContract()} {
		if ps := connectors.CheckContract(c); len(ps) > 0 {
			t.Fatalf("%s: %v", c.Ref(), ps)
		}
	}
	if ps := connectors.CheckType(caltest.Type(), func(ref string) (connectors.Contract, bool) {
		switch ref {
		case calendar.EventsRef:
			return calendar.EventsContract(), true
		case caltest.ControlRef:
			return caltest.ControlContract(), true
		}
		return connectors.Contract{}, false
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}

func TestFaultsAndFreeBusyAcrossConnections(t *testing.T) {
	s := caltest.New()
	a := open(t, s, "a", `{"email":"anna@example.org"}`)
	b := open(t, s, "b", `{"email":"ben@example.org"}`)
	ev, ctl := caller(a, calendar.EventsRef), caller(a, caltest.ControlRef)
	if _, err := ctl("fail", map[string]any{"operation": "event.create", "times": 1, "kind": "rateLimited"}); err != nil {
		t.Fatal(err)
	}
	create := map[string]any{"key": "k", "event": map[string]any{"title": "x", "start": map[string]string{"local": "2026-12-01T10:00:00", "zone": "UTC"}, "end": map[string]string{"local": "2026-12-01T11:00:00", "zone": "UTC"}}}
	_, err := ev("event.create", create)
	var ce *connectors.Error
	if !errors.As(err, &ce) || ce.Code != connectors.CodeRateLimited || !ce.Retryable() {
		t.Fatalf("%v", err)
	}
	if _, err := ev("event.create", create); err != nil {
		t.Fatal(err)
	}
	// Ben's connection sees Anna's busy time by her email.
	raw, err := caller(b, calendar.EventsRef)("freeBusy", map[string]any{"people": []string{"anna@example.org", "jo@example.com"},
		"window": map[string]string{"from": "2026-12-01T00:00:00Z", "to": "2026-12-02T00:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	var fb struct{ FreeBusy []calendar.FreeBusy }
	_ = json.Unmarshal(raw, &fb)
	if len(fb.FreeBusy) != 2 || len(fb.FreeBusy[0].Busy) != 1 || fb.FreeBusy[0].Busy[0].Start != "2026-12-01T10:00:00Z" || fb.FreeBusy[1].Error == "" {
		t.Fatalf("%s", raw)
	}
}

func TestWatchDropsAndCatchesUp(t *testing.T) {
	sess := open(t, caltest.New(), "c1", `{}`)
	ctl, ev := caller(sess, caltest.ControlRef), caller(sess, calendar.EventsRef)
	wctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var seen []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		c := ctxFor(calendar.EventsRef)
		c.Context = wctx
		_ = sess.(adapter.Watcher).Watch(c, nil, func(e adapter.Event) error {
			var p struct{ Event string }
			_ = json.Unmarshal(e.Payload, &p)
			mu.Lock()
			seen = append(seen, e.Name+":"+p.Event)
			mu.Unlock()
			return nil
		})
	}()
	add := func(title string) string {
		raw, err := ctl("add", map[string]any{"event": map[string]any{"title": title, "start": map[string]string{"date": "2026-12-01"}, "end": map[string]string{"date": "2026-12-02"}}})
		if err != nil {
			t.Fatal(err)
		}
		var r struct{ ID string }
		_ = json.Unmarshal(raw, &r)
		return r.ID
	}
	first := add("one")
	if _, err := ctl("dropNotifications", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	lost := add("two")
	third := add("three")
	if _, err := ctl("remove", map[string]any{"id": first}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	want := []string{"eventChanged:" + first, "eventChanged:" + third, "eventRemoved:" + first}
	if len(seen) != 3 || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("notices = %v, want %v", seen, want)
	}
	// The lost one is still in the delta.
	raw, _ := ev("event.list", map[string]any{"calendar": "primary", "cursor": "0"})
	var l struct {
		Events  []calendar.Event
		Removed []string
	}
	_ = json.Unmarshal(raw, &l)
	ids := map[string]bool{}
	for _, e := range l.Events {
		ids[e.ID] = true
	}
	if !ids[lost] || len(l.Removed) != 1 {
		t.Fatalf("%s", raw)
	}
}
