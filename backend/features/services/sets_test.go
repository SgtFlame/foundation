package services

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
)

// setService is a fixture service for processes with service sets.
type setService interface{ Who(setCallArgs) setResponse }

type setCallArgs struct {
	features.ServiceArgs
	Sleep time.Duration
}

type setResponse struct {
	Common  features.ServiceResponse
	Process string
}

type setReal struct {
	name  string
	calls atomic.Int32
}

func (w *setReal) Who(a setCallArgs) setResponse {
	w.calls.Add(1)
	if a.Sleep > 0 {
		time.Sleep(a.Sleep)
	}
	return setResponse{Common: features.OK(), Process: w.name}
}

type setProxy struct{ c *Client }

func (p setProxy) Who(a setCallArgs) setResponse {
	return Call[setResponse](p.c, "who", "Who", a, Idempotent())
}

func useWho(l *Locator, real func() setService) setService {
	return Use(l, "who", real, func(c *Client, _ setService) setService { return setProxy{c} },
		func(nc *nats.Conn, s setService, opts ...ServerOption) (*Server, error) {
			return Serve(nc, "who", map[string]Handler{"Who": Unary(s.Who)}, opts...)
		})
}

func whoCall(sleep time.Duration) setCallArgs {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	time.AfterFunc(3*time.Second, cancel)
	return setCallArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Sleep: sleep}
}

func TestParseSet(t *testing.T) {
	known := []string{"record", "ui", "chat", "embed"}
	g := Groups{"web": {"ui"}, "runner": {"chat", "embed"}}
	set, err := ParseSet("web, record,connectors:smtp", g, known)
	if err != nil || !set["ui"] || !set["record"] || !set["connectors:smtp"] || set["chat"] {
		t.Fatalf("set %v %v", set, err)
	}
	if got := Elsewhere(set, known); len(got) != 2 || got[0] != "chat" || got[1] != "embed" {
		t.Fatalf("elsewhere %v", got)
	}
	if set, err := ParseSet("all", g, known); set != nil || err != nil {
		t.Fatalf("all: %v %v", set, err)
	}
	if _, err := ParseSet("nope", g, known); err == nil {
		t.Fatal("an unknown name was taken")
	}
}

// A web process reaches a service a data process runs; with the data
// process stopped its calls answer 503 naming the service; two data
// processes share calls; a draining one finishes its call in flight
// while new calls go to the other.
func TestTwoProcesses(t *testing.T) {
	ns := natsd.StartTest(t)
	keys, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	connect := func(name string) *Client {
		nc, err := ns.Connect(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(nc.Close)
		return &Client{NC: nc, Caller: name, Keys: keys}
	}
	data := func(name string) (*Locator, *setReal) {
		real := &setReal{name: name}
		l := &Locator{Client: connect(name), Set: map[string]bool{"who": true}, Serve: true, ServerOptions: []ServerOption{VerifyWith(keys.Public())}}
		useWho(l, func() setService { return real })
		return l, real
	}
	web := &Locator{Client: connect("web"), Set: map[string]bool{"ui": true}, Serve: true}
	who := useWho(web, func() setService { t.Fatal("web made the real service"); return nil })
	if got := web.Proxied(); len(got) != 1 || got[0] != "who" {
		t.Fatalf("proxied %v", got)
	}
	if m := Missing(web.Client.NC, web.Proxied(), 100*time.Millisecond); len(m) != 1 {
		t.Fatalf("missing before the data process: %v", m)
	}

	d1, r1 := data("data-1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitReady(ctx, web.Client.NC, web.Proxied(), time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if r := who.Who(whoCall(0)); r.Process != "data-1" {
		t.Fatalf("served by %+v", r)
	}

	// Stopped: 503 naming the service, within the deadline
	d1.Stop()
	start := time.Now()
	r := who.Who(whoCall(0))
	if r.Common.Code != http.StatusServiceUnavailable || r.Common.ErrorCode != CodeUnavailable || time.Since(start) > 3*time.Second {
		t.Fatalf("with no data process: %+v after %s", r.Common, time.Since(start))
	}

	// Two data processes share calls
	d2, r2 := data("data-2")
	d3, r3 := data("data-3")
	_, _ = r1, d2
	for i := 0; i < 100; i++ {
		if r := who.Who(whoCall(0)); r.Common.Code != http.StatusOK {
			t.Fatalf("call %d: %+v", i, r.Common)
		}
	}
	if r2.calls.Load() == 0 || r3.calls.Load() == 0 {
		t.Fatalf("calls not shared: %d and %d", r2.calls.Load(), r3.calls.Load())
	}

	// Draining: a slow call held by data-3 completes; new calls go to data-2
	var wg sync.WaitGroup
	var slow setResponse
	before := r3.calls.Load()
	for r3.calls.Load() == before {
		// send slow calls until data-3 holds one
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := who.Who(whoCall(400 * time.Millisecond)); r.Process == "data-3" {
				slow = r
			}
		}()
		time.Sleep(20 * time.Millisecond)
	}
	if !d3.StopWithin(2 * time.Second) {
		t.Fatal("the drain did not finish")
	}
	wg.Wait()
	if slow.Common.Code != http.StatusOK {
		t.Fatalf("the call in flight at the drain: %+v", slow.Common)
	}
	n2 := r2.calls.Load()
	for i := 0; i < 10; i++ {
		if r := who.Who(whoCall(0)); r.Process != "data-2" {
			t.Fatalf("after the drain served by %+v", r)
		}
	}
	if r2.calls.Load() != n2+10 {
		t.Fatalf("data-2 calls %d → %d", n2, r2.calls.Load())
	}
}
