package hooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/connectors/hooks"
	"github.com/SgtFlame/foundation/backend/features/connectors/hooks/verify"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

const secret = "whsec_c2VjcmV0LWZvci10ZXN0cw=="

// hookType receives test.echo@1's finished event by callback; its
// connection's settings name the scheme, so one type tries them all.
func hookType() connectors.Type {
	return connectors.Type{ID: "test.hook", Contracts: []string{connectorstest.ContractRef}, Source: connectors.SourceBuiltin,
		Receives: "hmac-sha256", Settings: json.RawMessage(`{"type":"object","properties":{"scheme":{"type":"string","title":"Scheme"},"secret":{"type":"secret","title":"Secret"}}}`),
		Meta: connectors.Meta{Title: "Test hook", Description: "Receives test.echo@1 callbacks. For tests only.", Tags: []string{"test"}}}
}

type receiver struct{}

func (receiver) Receive(ctx adapter.Context, conn adapter.Connection, cb adapter.Callback) ([]adapter.Event, error) {
	var st struct{ Scheme, Secret string }
	_ = json.Unmarshal(conn.Settings, &st)
	if err := verify.Verify(st.Scheme, st.Secret, cb.Header, cb.Body, time.Now()); err != nil {
		return nil, connectors.Errorf(connectors.CodeAuth, "%v", err)
	}
	var p struct{ ID string }
	if err := json.Unmarshal(cb.Body, &p); err != nil || p.ID == "" {
		return nil, connectors.Errorf(connectors.CodeInvalid, "not an event")
	}
	return []adapter.Event{{Contract: connectorstest.ContractRef, Name: "finished", ID: p.ID, Payload: cb.Body}}, nil
}

// flaky books fail the next publishes while failing > 0.
type flaky struct {
	connectors.Books
	failing atomic.Int32
}

func (f *flaky) Publish(ctx context.Context, tenant uint64, connection, contract, event, id string, payload json.RawMessage, at time.Time) error {
	if f.failing.Add(-1) >= 0 {
		return errors.New("jetstream is away")
	}
	return f.Books.Publish(ctx, tenant, connection, contract, event, id, payload, at)
}

type fixture struct {
	t      *testing.T
	srv    *natsd.Server
	books  *flaky
	scheme string
	hs     *hooks.Server
}

func setup(t *testing.T, scheme string) *fixture {
	srv := natsd.StartTest(t)
	f := &fixture{t: t, srv: srv, scheme: scheme, books: &flaky{Books: connectors.InJetStream(srv.JS())}}
	nc, err := srv.Connect("hook-adapter")
	if err != nil {
		t.Fatal(err)
	}
	o := adapter.Options{Name: "hooks", Types: []connectors.Type{hookType()}, Contracts: []connectors.Contract{connectorstest.Contract()},
		Impls: []adapter.Impl{{Type: "test.hook", Receive: receiver{}, Open: func(adapter.Context, adapter.Connection) (adapter.Session, error) {
			return nil, connectors.Errorf(connectors.CodeNotSupported, "no calls")
		}}},
		Books: f.books,
		Resolve: func(_ context.Context, tenant uint64, typ, connection string) (adapter.Connection, error) {
			if connection != "c1" {
				return adapter.Connection{}, connectors.Errorf(connectors.CodeNotFound, "no connection %s", connection)
			}
			s, _ := json.Marshal(map[string]string{"scheme": f.scheme, "secret": secret})
			return adapter.Connection{ID: connection, Type: typ, Tenant: tenant, Settings: s}, nil
		}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- adapter.Serve(ctx, nc, o) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
		nc.Close()
	})
	f.hs = &hooks.Server{NC: srv.Conn(), JS: srv.JS()}
	// wait until the adapter answers on cx.receive
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := srv.Conn().Request(connectors.ReceiveSubject(1, "test.hook", "c1"), []byte(`{}`), time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the adapter does not receive: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return f
}

func (f *fixture) hook() hooks.Hook {
	f.t.Helper()
	h, err := hooks.Ensure(context.Background(), f.srv.JS(), 1, "c1", "test.hook")
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

func (f *fixture) post(id string, h http.Header, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, hooks.Path+id, bytes.NewReader(body))
	for k, v := range h {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	f.hs.ServeHTTP(w, r)
	return w
}

func (f *fixture) events() uint64 {
	f.t.Helper()
	st, err := connectors.EventStream(context.Background(), f.srv.JS(), 1)
	if err != nil {
		f.t.Fatal(err)
	}
	info, err := st.Info(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return info.State.Msgs
}

func TestSchemes(t *testing.T) {
	for _, scheme := range connectors.Receivers {
		t.Run(scheme, func(t *testing.T) {
			f := setup(t, scheme)
			h := f.hook()
			body := []byte(`{"id":"evt_1","jobId":"j1"}`)
			if w := f.post(h.ID, verify.Sign(scheme, secret, body, time.Now()), body); w.Code != 200 {
				t.Fatalf("signed: %d %s", w.Code, w.Body)
			}
			if n := f.events(); n != 1 {
				t.Fatalf("events: %d", n)
			}
			bad := verify.Sign(scheme, "whsec_b3RoZXI=", body, time.Now())
			if w := f.post(h.ID, bad, []byte(`{"id":"evt_2","jobId":"j2"}`)); w.Code != 400 {
				t.Fatalf("bad signature: %d", w.Code)
			}
			if scheme != "github" { // GitHub signs no time
				old := []byte(`{"id":"evt_3","jobId":"j3"}`)
				if w := f.post(h.ID, verify.Sign(scheme, secret, old, time.Now().Add(-time.Hour)), old); w.Code != 400 {
					t.Fatalf("expired: %d", w.Code)
				}
			}
			if n := f.events(); n != 1 {
				t.Fatalf("refused callbacks made events: %d", n)
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	f := setup(t, "hmac-sha256")
	h := f.hook()
	if again := f.hook(); again.ID != h.ID {
		t.Fatalf("a connection got two hooks: %s %s", h.ID, again.ID)
	}
	body := []byte(`{"id":"evt_1","jobId":"j1"}`)
	if w := f.post(hooks.NewID(), verify.Sign("hmac-sha256", secret, body, time.Now()), body); w.Code != 404 {
		t.Fatalf("unknown hook: %d", w.Code)
	}
	if f.hs.Unknown.Load() != 1 {
		t.Fatalf("unknown hooks counted: %d", f.hs.Unknown.Load())
	}
	big := []byte(`{"id":"evt_big","pad":"` + strings.Repeat("x", 1100*1024) + `"}`)
	if w := f.post(h.ID, verify.Sign("hmac-sha256", secret, big, time.Now()), big); w.Code != 413 {
		t.Fatalf("oversized: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, hooks.Path+h.ID, nil)
	w := httptest.NewRecorder()
	f.hs.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatalf("GET: %d", w.Code)
	}
	if n := f.events(); n != 0 {
		t.Fatalf("events: %d", n)
	}
	// a dropped hook is gone
	if err := hooks.Drop(context.Background(), f.srv.JS(), 1, "c1"); err != nil {
		t.Fatal(err)
	}
	if w := f.post(h.ID, verify.Sign("hmac-sha256", secret, body, time.Now()), body); w.Code != 404 {
		t.Fatalf("dropped hook: %d", w.Code)
	}
}

func TestOnceOnly(t *testing.T) {
	f := setup(t, "standard-webhooks")
	h := f.hook()
	body := []byte(`{"id":"evt_1","jobId":"j1"}`)
	sig := verify.Sign("standard-webhooks", secret, body, time.Now())
	// a failed publish answers 503, and the service's retry is kept once
	f.books.failing.Store(1)
	if w := f.post(h.ID, sig, body); w.Code != 503 {
		t.Fatalf("failed publish: %d", w.Code)
	}
	if n := f.events(); n != 0 {
		t.Fatalf("events after a failed publish: %d", n)
	}
	for i := 0; i < 10; i++ {
		if w := f.post(h.ID, sig, body); w.Code != 200 {
			t.Fatalf("repeat %d: %d %s", i, w.Code, w.Body)
		}
	}
	// two at once
	other := []byte(`{"id":"evt_2","jobId":"j2"}`)
	osig := verify.Sign("standard-webhooks", secret, other, time.Now())
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := f.post(h.ID, osig, other); w.Code != 200 {
				t.Errorf("concurrent: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if n := f.events(); n != 2 {
		t.Fatalf("events: %d, want 2", n)
	}
}

func TestRateLimit(t *testing.T) {
	f := setup(t, "hmac-sha256")
	f.hs.Limits = func(context.Context) hooks.Limits { return hooks.Limits{RatePerSecond: 0.001, Burst: 2} }
	h := f.hook()
	codes := []int{}
	for i := 0; i < 3; i++ {
		body := []byte(`{"id":"evt_` + string(rune('a'+i)) + `"}`)
		w := f.post(h.ID, verify.Sign("hmac-sha256", secret, body, time.Now()), body)
		codes = append(codes, w.Code)
		if w.Code == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("429 without Retry-After")
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Fatalf("codes: %v", codes)
	}
}
