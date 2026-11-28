package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Server serves the hooks. It is an http.Handler for Path, mounted outside
// /api/v1 and its session middleware: a hook's caller is a service, which
// proves itself by the signature its connection's type checks.
type Server struct {
	// NC asks the adapters on cx.receive; JS keeps cx_hooks and
	// cx_hook_rates (the services account's, which connectors use).
	NC *nats.Conn
	JS jetstream.JetStream
	// Limits gives the limits now (the connectors.hooks section); Defaults
	// when nil.
	Limits func(ctx context.Context) Limits
	// Now is the clock, for tests.
	Now func() time.Time
	// Unknown counts requests to hooks that do not exist.
	Unknown atomic.Int64
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) limits(ctx context.Context) Limits {
	if s.Limits == nil {
		return Defaults
	}
	return s.Limits(ctx).withDefaults()
}

// answer writes a short plain reply: services read only the status.
func answer(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text+"\n")
}

// ServeHTTP takes one callback: find the hook, check the body's size and
// the hook's rate, have its type verify and publish it, answer.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		answer(w, http.StatusMethodNotAllowed, "hooks take POST")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, Path)
	lim := s.limits(ctx)
	max := int64(lim.MaxBodyKB) * 1024
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			answer(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the body is over %d KB", lim.MaxBodyKB))
			return
		}
		answer(w, http.StatusBadRequest, "the body could not be read")
		return
	}
	kv, err := KV(ctx, s.JS)
	if err != nil {
		log.Error(ctx, err, "the hooks cannot be read")
		answer(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	h, ok, err := Lookup(ctx, kv, id)
	if err != nil {
		log.Error(ctx, err, "a hook cannot be read")
		answer(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	if !ok {
		s.Unknown.Add(1)
		log.Info(ctx, "a callback came for an unknown hook")
		answer(w, http.StatusNotFound, "no such hook")
		return
	}
	b := log.With("tenant", h.Tenant, "connection", h.Connection, "type", h.Type)
	rates, err := kvOf(ctx, s.JS, RatesBucket, "Connector hooks' token buckets (forge 26b-1)", time.Hour)
	if err == nil {
		var allowed bool
		var wait time.Duration
		allowed, wait, err = take(ctx, rates, h.ID, lim.RatePerSecond, float64(lim.Burst), s.now())
		if err == nil && !allowed {
			w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
			answer(w, http.StatusTooManyRequests, "too many callbacks; send it again later")
			return
		}
	}
	if err != nil {
		b.Warn(ctx, "a hook's rate could not be checked: %v", err) // not a reason to refuse
	}
	cb := adapter.Callback{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body, ReceivedAt: s.now().UTC()}
	raw, _ := json.Marshal(cb)
	rctx, cancel := context.WithTimeout(ctx, adapter.ReceiveTimeout+2*time.Second)
	defer cancel()
	msg, err := s.NC.RequestWithContext(rctx, connectors.ReceiveSubject(h.Tenant, h.Type, h.Connection), raw)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			b.Warn(ctx, "no adapter receives callbacks for %s", h.Type)
		} else {
			b.Warn(ctx, "a callback was not answered: %v", err)
		}
		answer(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	var rep connectors.Reply
	if err := json.Unmarshal(msg.Data, &rep); err != nil {
		answer(w, http.StatusServiceUnavailable, "try again later")
		return
	}
	if rep.Error != nil {
		switch rep.Error.Code {
		case connectors.CodeInvalid, connectors.CodeAuth, connectors.CodeNotSupported:
			b.Info(ctx, "a callback was refused: %s", rep.Error.Code) // never its body
			answer(w, http.StatusBadRequest, "refused")
		case connectors.CodeRateLimited:
			answer(w, http.StatusTooManyRequests, "send it again later")
		default:
			answer(w, http.StatusServiceUnavailable, "try again later")
		}
		return
	}
	answer(w, http.StatusOK, "ok")
}
