package services

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

// Handler runs one operation from its request body with the call's
// ServiceArgs and returns its response; an error means the body could not
// be read (400 service_bad_request).
type Handler func(a features.ServiceArgs, body []byte) (any, error)

// Unary is the Handler of an operation fn taking args A (embedding
// features.ServiceArgs) and returning response R.
func Unary[A, R any](fn func(A) R) Handler {
	mustStruct[A]()
	return func(a features.ServiceArgs, body []byte) (any, error) {
		var args A
		if err := Decode(body, &args); err != nil {
			return nil, err
		}
		setArgs(&args, a)
		return fn(args), nil
	}
}

// ServerOption changes how a server reads its calls.
type ServerOption func(*Server)

// TrustHeaders makes the server take the principal a call's headers name.
// Only tests and loopback use it, until the call token (forge 21b).
func TrustHeaders() ServerOption { return func(s *Server) { s.trust = true } }

// VerifyWith makes the server accept call tokens signed by any of public
// (forge 21b): Keys.Public of the processes that make them.
func VerifyWith(public []ed25519.PublicKey) ServerOption {
	return func(s *Server) { s.public = public }
}

// Concurrency bounds how many calls the server runs at once (256 when
// unset).
func Concurrency(n int) ServerOption { return func(s *Server) { s.slots = make(chan struct{}, n) } }

// Server serves one service's calls on NATS.
type Server struct {
	service string
	ops     map[string]Handler
	trust   bool
	public  []ed25519.PublicKey
	slots   chan struct{}
	nc      *nats.Conn
	sub     *nats.Subscription
	cancel  *nats.Subscription
	cancels cancels
	// streams are the streams in flight, ended by Stop: a stream is
	// long-lived, so a server stopping hands it back to its caller as lost
	// rather than waiting for it (forge 21e-1).
	streams cancels
	// at is the one operation of a server made by ServeAt.
	at string
	wg sync.WaitGroup
}

// Serve subscribes to service's calls in its queue group and runs each
// with the operation of ops its subject names.
func Serve(nc *nats.Conn, service string, ops map[string]Handler, opts ...ServerOption) (*Server, error) {
	s := &Server{service: service, ops: ops, nc: nc}
	for _, o := range opts {
		o(s)
	}
	if s.slots == nil {
		s.slots = make(chan struct{}, 256)
	}
	// Cancellations go to every server: the one holding the call stops it.
	csub, err := nc.Subscribe(CancelSubject(service, "*"), func(m *nats.Msg) {
		s.cancels.cancel(OpOf(m.Subject))
	})
	if err != nil {
		return nil, log.Wrap(err)
	}
	s.cancel = csub
	sub, err := nc.QueueSubscribe(Prefix+"."+service+".>", Queue(service), func(m *nats.Msg) {
		if isCancel(m.Subject) {
			return
		}
		s.slots <- struct{}{}
		s.wg.Add(1)
		go func() {
			defer func() { <-s.slots; s.wg.Done() }()
			s.handle(m)
		}()
	})
	if err != nil {
		_ = csub.Unsubscribe()
		return nil, log.Wrap(err)
	}
	s.sub = sub
	return s, nil
}

// ServeAt serves the one call sent to subject (a WaitSubject) with
// handler: the process waiting for it, alone, not in a queue group.
func ServeAt(nc *nats.Conn, subject, service string, handler Handler, opts ...ServerOption) (*Server, error) {
	op := OpOf(subject)
	s := &Server{service: service, ops: map[string]Handler{op: handler}, nc: nc, at: op}
	for _, o := range opts {
		o(s)
	}
	if s.slots == nil {
		s.slots = make(chan struct{}, 4)
	}
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		s.slots <- struct{}{}
		s.wg.Add(1)
		go func() {
			defer func() { <-s.slots; s.wg.Done() }()
			s.handle(m)
		}()
	})
	if err != nil {
		return nil, log.Wrap(err)
	}
	s.sub = sub
	return s, nil
}

// Stop leaves the queue group, ends the streams in flight and waits for
// the calls in flight.
func (s *Server) Stop() {
	if s.sub != nil {
		_ = s.sub.Drain()
	}
	s.streams.all()
	s.wg.Wait()
	if s.cancel != nil {
		_ = s.cancel.Unsubscribe()
	}
}

func (s *Server) handle(m *nats.Msg) {
	start := time.Now()
	op := OpOf(m.Subject)
	h, err := ReadHeaders(m.Header)
	ctx := context.Background()
	if err != nil {
		s.reply(ctx, m, op, start, failure{Failed(http.StatusBadRequest, CodeBadRequest, "The call's headers cannot be read.")})
		return
	}
	b := logging.Trace(h.Trace)
	if h.Tenant != 0 {
		b = b.Tenant(strconv.FormatUint(h.Tenant, 10))
	}
	p := features.AnonymousPrincipal()
	var tok *Token
	if h.Token != "" {
		t, err := Verify(h.Token, s.public, time.Now())
		switch {
		case errors.Is(err, ErrExpired):
			s.reply(ctx, m, op, start, failure{Failed(http.StatusUnauthorized, CodeExpired, "The call's token expired.")})
			return
		case err != nil:
			log.Warn(ctx, "svc.call %s.%s refused: %v", s.service, op, err)
			s.reply(ctx, m, op, start, failure{Failed(http.StatusUnauthorized, CodeUntrusted, "Who the call is for cannot be trusted.")})
			return
		}
		tok = &t
		p = t.PrincipalOf()
		if p.TenantID != 0 {
			b = b.Tenant(strconv.FormatUint(p.TenantID, 10))
		}
		if p.UserID != 0 {
			b = b.User(strconv.FormatUint(p.UserID, 10))
		}
	} else if h.Principal != nil {
		if !s.trust {
			s.reply(ctx, m, op, start, failure{Failed(http.StatusUnauthorized, CodeUntrusted, "Who the call is for cannot be trusted.")})
			return
		}
		p = h.Principal.In(h.Tenant)
		if p.UserID != 0 {
			b = b.User(strconv.FormatUint(p.UserID, 10))
		}
	}
	ctx = logging.WithContext(ctx, b)
	if tok != nil {
		ctx = WithToken(ctx, *tok)
	}
	ctx, cancel := context.WithCancel(ctx)
	if !h.Deadline.IsZero() {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, h.Deadline)
		defer stop()
	}
	defer cancel()
	if h.CallID != "" {
		s.cancels.add(h.CallID, cancel)
		defer s.cancels.remove(h.CallID)
	}

	handler, ok := s.ops[op]
	if !ok {
		s.reply(ctx, m, op, start, failure{Failed(http.StatusNotImplemented, CodeUnknownOp, fmt.Sprintf("The %s service has no operation %s.", s.service, op))})
		return
	}
	resp, err := handler(features.ServiceArgs{Ctx: ctx, Principal: p, Meta: h.Meta}, m.Data)
	if err != nil {
		s.reply(ctx, m, op, start, failure{Failed(http.StatusBadRequest, CodeBadRequest, "The call's arguments cannot be read.")})
		return
	}
	if st, ok := resp.(streamOf); ok {
		id := nuid.Next()
		s.streams.add(id, cancel)
		s.serveStream(ctx, m, op, st.req)
		s.streams.remove(id)
		log.With("service", s.service, "op", op, "durationMs", time.Since(start).Milliseconds(), "side", "server").
			Debug(ctx, "svc.stream %s.%s", s.service, op)
		return
	}
	if ctx.Err() != nil {
		// The caller gave up (its deadline passed or it canceled): it
		// already answered itself, so no reply races its own.
		log.With("service", s.service, "op", op, "durationMs", time.Since(start).Milliseconds(), "side", "server").
			Debug(ctx, "svc.call %s.%s ended after its caller gave up", s.service, op)
		return
	}
	s.reply(ctx, m, op, start, resp)
}

// isCancel reports whether subject is a call's cancellation or a call to
// a waiting process (WaitSubject), which the queue group leaves alone.
func isCancel(subject string) bool {
	parts := strings.SplitN(subject, ".", 4)
	return len(parts) == 4 && (parts[2] == "cancel" || parts[2] == "wait")
}

// WaitSubject is where one process waits for a call about one thing, such
// as the answer to a tool call its chat round is paused on (forge 21c):
// svc.<service>.wait.<key>…, each key a token. Send to it with CallAt and
// serve it with ServeAt.
func WaitSubject(service string, keys ...string) string {
	out := Prefix + "." + service + ".wait"
	for _, k := range keys {
		out += "." + SubjectToken(k)
	}
	return out
}

// SubjectToken makes s a single subject token: '.', '*', '>' and spaces become
// '_' (an empty s is "_").
func SubjectToken(s string) string {
	if s == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\r', '\n':
			return '_'
		}
		return r
	}, s)
}

func (s *Server) reply(ctx context.Context, m *nats.Msg, op string, start time.Time, resp any) {
	body, err := Encode(resp)
	if err != nil {
		log.Error(ctx, err, "svc.call %s.%s: the response cannot be encoded", s.service, op)
		body, _ = Encode(failure{Failed(http.StatusInternalServerError, "service_bad_response", "")})
	}
	if m.Reply != "" {
		if err := m.Respond(body); err != nil {
			log.Warn(ctx, "svc.call %s.%s: the reply was not sent: %v", s.service, op, err)
		}
	}
	log.With("service", s.service, "op", op, "code", CommonOf(resp).Code, "durationMs", time.Since(start).Milliseconds(), "side", "server").
		Debug(ctx, "svc.call %s.%s", s.service, op)
}
