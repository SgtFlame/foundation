package services

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
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

// Concurrency bounds how many calls the server runs at once (256 when
// unset).
func Concurrency(n int) ServerOption { return func(s *Server) { s.slots = make(chan struct{}, n) } }

// Server serves one service's calls on NATS.
type Server struct {
	service string
	ops     map[string]Handler
	trust   bool
	slots   chan struct{}
	sub     *nats.Subscription
	wg      sync.WaitGroup
}

// Serve subscribes to service's calls in its queue group and runs each
// with the operation of ops its subject names.
func Serve(nc *nats.Conn, service string, ops map[string]Handler, opts ...ServerOption) (*Server, error) {
	s := &Server{service: service, ops: ops}
	for _, o := range opts {
		o(s)
	}
	if s.slots == nil {
		s.slots = make(chan struct{}, 256)
	}
	sub, err := nc.QueueSubscribe(Prefix+"."+service+".>", Queue(service), func(m *nats.Msg) {
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

// Stop leaves the queue group and waits for calls in flight.
func (s *Server) Stop() {
	if s.sub != nil {
		_ = s.sub.Drain()
	}
	s.wg.Wait()
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
	if h.Principal != nil {
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
	cancel := func() {}
	if !h.Deadline.IsZero() {
		ctx, cancel = context.WithDeadline(ctx, h.Deadline)
	}
	defer cancel()

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
	s.reply(ctx, m, op, start, resp)
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
