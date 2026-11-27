package services

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

type echoArgs struct {
	features.ServiceArgs
	Text  string
	Sleep time.Duration
	Fail  bool
}

type echoResponse struct {
	Common    features.ServiceResponse
	Text      string
	User      uint64
	Tenant    uint64
	Feature   string
	Trace     string
	Deadlined bool
}

func echo(calls *atomic.Int32) func(echoArgs) echoResponse {
	return func(a echoArgs) echoResponse {
		calls.Add(1)
		if a.Sleep > 0 {
			select {
			case <-time.After(a.Sleep):
			case <-a.Context().Done():
			}
		}
		if a.Fail {
			err := logging.Code("echo_refused").Public("Echo refuses.").Errorf("refused %q", a.Text)
			return echoResponse{Common: *a.Args().Fail(http.StatusConflict, err)}
		}
		_, has := a.Context().Deadline()
		return echoResponse{Common: features.OK(), Text: a.Text, User: a.Principal.UserID, Tenant: a.Principal.TenantID,
			Feature: a.Meta.Feature, Trace: logging.TraceFrom(a.Context()), Deadlined: has}
	}
}

// Args is the embedded ServiceArgs, for the fixture.
func (a echoArgs) Args() features.ServiceArgs { return a.ServiceArgs }

func TestCallRoundTrip(t *testing.T) {
	ns := natsd.StartTest(t)
	var calls atomic.Int32
	srv, err := Serve(ns.Conn(), "echo", map[string]Handler{"Echo": Unary(echo(&calls))}, TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	c := &Client{NC: ns.Conn(), Caller: "test"}
	ctx := logging.WithContext(context.Background(), logging.Trace("trace-1"))
	args := features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal(), Meta: features.Meta{Feature: "f"}}

	r := Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: args, Text: "hi"})
	if r.Common.Code != http.StatusOK || r.Text != "hi" || r.User != 1 || r.Tenant != 1 || r.Feature != "f" || r.Trace != "trace-1" || !r.Deadlined {
		t.Fatalf("response %+v", r)
	}
	r = Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: args, Text: "no", Fail: true})
	if r.Common.Code != http.StatusConflict || r.Common.Message != "Echo refuses." || r.Common.ErrorCode != "echo_refused" || r.Common.Trace != "" {
		t.Fatalf("failure %+v", r.Common)
	}
	r = Call[echoResponse](c, "echo", "Nope", echoArgs{ServiceArgs: args})
	if r.Common.Code != http.StatusNotImplemented || r.Common.ErrorCode != CodeUnknownOp {
		t.Fatalf("unknown op %+v", r.Common)
	}
}

func TestTransportFailures(t *testing.T) {
	ns := natsd.StartTest(t)
	c := &Client{NC: ns.Conn(), Retries: -1}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	r := Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: args})
	if r.Common.Code != http.StatusServiceUnavailable || r.Common.ErrorCode != CodeUnavailable || r.Common.Message != "The echo service is unavailable." {
		t.Fatalf("no responders %+v", r.Common)
	}

	var calls atomic.Int32
	srv, err := Serve(ns.Conn(), "echo", map[string]Handler{"Echo": Unary(echo(&calls))}, TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r = Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Sleep: time.Second})
	if r.Common.Code != http.StatusGatewayTimeout || r.Common.ErrorCode != CodeTimeout {
		t.Fatalf("deadline %+v", r.Common)
	}
}

func TestRetriesOnlyIdempotent(t *testing.T) {
	ns := natsd.StartTest(t)
	var calls atomic.Int32
	start := func() {
		time.Sleep(150 * time.Millisecond)
		srv, err := Serve(ns.Conn(), "late", map[string]Handler{"Echo": Unary(echo(&calls))}, TrustHeaders())
		if err != nil {
			t.Error(err)
			return
		}
		t.Cleanup(srv.Stop)
	}
	go start()
	c := &Client{NC: ns.Conn()}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	// Not idempotent: no server yet, so 503 at once.
	if r := Call[echoResponse](c, "late", "Echo", echoArgs{ServiceArgs: args}); r.Common.Code != http.StatusServiceUnavailable {
		t.Fatalf("plain call %+v", r.Common)
	}
	// Idempotent: retried until the server is up.
	if r := Call[echoResponse](c, "late", "Echo", echoArgs{ServiceArgs: args, Text: "x"}, Idempotent()); r.Common.Code != http.StatusOK || r.Text != "x" {
		t.Fatalf("idempotent call %+v", r.Common)
	}
}

func TestUntrustedPrincipal(t *testing.T) {
	ns := natsd.StartTest(t)
	var calls atomic.Int32
	srv, err := Serve(ns.Conn(), "echo", map[string]Handler{"Echo": Unary(echo(&calls))})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	c := &Client{NC: ns.Conn()}
	r := Call[echoResponse](c, "echo", "Echo", echoArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}})
	if r.Common.Code != http.StatusUnauthorized || r.Common.ErrorCode != CodeUntrusted || calls.Load() != 0 {
		t.Fatalf("untrusted %+v, calls %d", r.Common, calls.Load())
	}
	// A call naming no principal runs as anonymous.
	r = Call[echoResponse](c, "echo", "Echo", echoArgs{Text: "a"})
	if r.Common.Code != http.StatusOK || r.User != 0 {
		t.Fatalf("anonymous %+v", r)
	}
}

func TestEncodeDropsServiceArgs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, err := Encode(echoArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Text: "t"})
	if err != nil {
		t.Fatal(err)
	}
	var back echoArgs
	if err := Decode(b, &back); err != nil || back.Text != "t" || back.Principal.Kind != "" {
		t.Fatalf("decoded %+v %v from %s", back, err, b)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
