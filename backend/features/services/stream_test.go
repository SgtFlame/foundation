package services

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

type countArgs struct {
	features.ServiceArgs
	N int
	// Hold keeps the stream open after N items until the call's context
	// ends.
	Hold bool
	Fail bool
}

type countDone struct {
	Common features.ServiceResponse
	Total  int
}

type countRequest struct {
	Common   features.ServiceResponse
	Name     string
	Items    <-chan int
	Response <-chan countDone
}

// count is a streamed operation: N items, then its result.
func count(ended *atomic.Int32) func(countArgs) *countRequest {
	return func(a countArgs) *countRequest {
		if a.Fail {
			return &countRequest{Common: Failed(http.StatusConflict, "count_refused", "Count refuses.")}
		}
		items := make(chan int)
		resp := make(chan countDone, 1)
		go func() {
			defer close(items)
			defer ended.Add(1)
			ctx := a.Context()
			for i := 0; i < a.N; i++ {
				select {
				case items <- i:
				case <-ctx.Done():
					return
				}
			}
			if a.Hold {
				<-ctx.Done()
				return
			}
			resp <- countDone{Common: features.OK(), Total: a.N}
		}()
		return &countRequest{Common: features.OK(), Name: "count", Items: items, Response: resp}
	}
}

func serveCount(t *testing.T, ns *natsd.Server, ended *atomic.Int32) *Server {
	t.Helper()
	srv, err := Serve(ns.Conn(), "count", map[string]Handler{"Count": Streamed(count(ended))}, TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

func countArgsFor(ctx context.Context, n int) countArgs {
	return countArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, N: n}
}

func TestStreamDeliversInOrder(t *testing.T) {
	ns := natsd.StartTest(t)
	var ended atomic.Int32
	serveCount(t, ns, &ended)
	c := &Client{NC: ns.Conn(), Caller: "test"}

	// More than the window, so the caller's acks are needed.
	n := 5 * Window
	req := Stream[*countRequest](c, "count", "Count", countArgsFor(context.Background(), n))
	if req.Common.Code != http.StatusOK || req.Name != "count" {
		t.Fatalf("head %+v", req)
	}
	got := 0
	for v := range req.Items {
		if v != got {
			t.Fatalf("item %d is %d", got, v)
		}
		got++
	}
	if got != n {
		t.Fatalf("got %d items", got)
	}
	done, ok := <-req.Response
	if !ok || done.Common.Code != http.StatusOK || done.Total != n {
		t.Fatalf("result %+v %v", done, ok)
	}
	if _, ok := <-req.Response; ok {
		t.Fatal("the response channel stays open")
	}
}

func TestStreamFailures(t *testing.T) {
	ns := natsd.StartTest(t)
	c := &Client{NC: ns.Conn(), Caller: "test", Timeout: time.Second}

	req := Stream[*countRequest](c, "count", "Count", countArgsFor(context.Background(), 1))
	if req.Common.Code != http.StatusServiceUnavailable || req.Common.ErrorCode != CodeUnavailable {
		t.Fatalf("no server: %+v", req.Common)
	}
	if r, ok := <-req.Response; !ok || r.Common.ErrorCode != CodeUnavailable {
		t.Fatalf("no server's response %+v", r)
	}
	if _, ok := <-req.Items; ok {
		t.Fatal("items open with no server")
	}

	var ended atomic.Int32
	serveCount(t, ns, &ended)
	a := countArgsFor(context.Background(), 1)
	a.Fail = true
	req = Stream[*countRequest](c, "count", "Count", a)
	if req.Common.Code != http.StatusConflict || req.Common.ErrorCode != "count_refused" {
		t.Fatalf("refused head %+v", req.Common)
	}
	if _, ok := <-req.Items; ok {
		t.Fatal("a refused stream's items are open")
	}
}

func TestStreamCanceledByCaller(t *testing.T) {
	ns := natsd.StartTest(t)
	var ended atomic.Int32
	// Two servers: the cancellation reaches the one holding the call.
	serveCount(t, ns, &ended)
	serveCount(t, ns, &ended)
	c := &Client{NC: ns.Conn(), Caller: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	a := countArgsFor(ctx, 3)
	a.Hold = true
	req := Stream[*countRequest](c, "count", "Count", a)
	for i := 0; i < 3; i++ {
		<-req.Items
	}
	cancel()
	waitFor(t, func() bool { return ended.Load() == 1 }, "the server's stream to end")
	for range req.Items {
	}
	if r, ok := <-req.Response; ok {
		t.Fatalf("a canceled stream's response %+v", r)
	}
}

func TestStreamLostWhenServerGoes(t *testing.T) {
	ns := natsd.StartTestListening(t)
	sc, err := ns.Connect("server")
	if err != nil {
		t.Fatal(err)
	}
	var ended atomic.Int32
	srv, err := Serve(sc, "count", map[string]Handler{"Count": Streamed(count(&ended))}, TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	_ = srv
	idle := streamIdle
	streamIdle = time.Second
	defer func() { streamIdle = idle }()
	c := &Client{NC: ns.Conn(), Caller: "test"}
	a := countArgsFor(context.Background(), 1)
	a.Hold = true
	req := Stream[*countRequest](c, "count", "Count", a)
	<-req.Items
	sc.Close() // the process dies: no done, no heartbeat
	select {
	case r := <-req.Response:
		if r.Common.Code != http.StatusServiceUnavailable || r.Common.ErrorCode != CodeLost {
			t.Fatalf("lost: %+v", r.Common)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the lost stream never ended")
	}
}

func TestCallCanceledReachesServer(t *testing.T) {
	ns := natsd.StartTest(t)
	var calls atomic.Int32
	stopped := make(chan struct{}, 1)
	slow := func(a echoArgs) echoResponse {
		calls.Add(1)
		select {
		case <-a.Context().Done():
			stopped <- struct{}{}
		case <-time.After(10 * time.Second):
		}
		return echoResponse{Common: features.OK()}
	}
	for i := 0; i < 2; i++ {
		srv, err := Serve(ns.Conn(), "echo", map[string]Handler{"Echo": Unary(slow)}, TrustHeaders())
		if err != nil {
			t.Fatal(err)
		}
		defer srv.Stop()
	}
	c := &Client{NC: ns.Conn(), Caller: "test"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitFor(t, func() bool { return calls.Load() == 1 }, "the call to start")
		cancel()
	}()
	r := Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}})
	if r.Common.Code != 499 {
		t.Fatalf("canceled call %+v", r.Common)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the server's call kept running")
	}
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
