package services

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
)

var log = logging.In("services")

// DefaultTimeout bounds a call whose context has no deadline.
const DefaultTimeout = 30 * time.Second

// Client is what a proxy sends its calls with.
type Client struct {
	NC *nats.Conn
	// Caller names the calling process or service in the call's headers,
	// for the server's logs.
	Caller string
	// Timeout bounds a call whose context has no deadline (DefaultTimeout
	// when 0).
	Timeout time.Duration
	// Retries is how many times an idempotent call is sent again when no
	// server answers (3 when 0; -1 for none).
	Retries int
}

// CallOption changes one call.
type CallOption func(*callOptions)

type callOptions struct {
	idempotent bool
	key        string
}

// Idempotent marks the operation as safe to send twice: it is retried
// when no server answers.
func Idempotent() CallOption { return func(o *callOptions) { o.idempotent = true } }

// Key sends the operation's idempotency key, and makes it Idempotent.
func Key(key string) CallOption {
	return func(o *callOptions) { o.idempotent, o.key = true, key }
}

// Call sends op of service with args (an operation's <Op>Args, embedding
// features.ServiceArgs) and returns its response R (an <Op>Response with a
// Common field). A failure of the call itself is a response too: 503
// service_unavailable when no server answers, 504 service_timeout when the
// deadline passes, 502 service_bad_reply when the reply does not decode.
func Call[R any](c *Client, service, op string, args any, opts ...CallOption) R {
	mustStruct[R]()
	var o callOptions
	for _, opt := range opts {
		opt(&o)
	}
	a := ArgsOf(args)
	ctx := a.Context()
	start := time.Now()
	var resp R
	finish := func(common features.ServiceResponse, err error) R {
		if err != nil || common.Code != 0 {
			SetCommon(&resp, common)
		}
		code := CommonOf(resp).Code
		b := log.With("service", service, "op", op, "code", code, "durationMs", time.Since(start).Milliseconds(), "side", "caller")
		if err != nil {
			b.Warn(ctx, "svc.call %s.%s failed: %v", service, op, err)
		} else {
			b.Debug(ctx, "svc.call %s.%s", service, op)
		}
		return resp
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	body, err := Encode(args)
	if err != nil {
		return finish(Failed(http.StatusBadRequest, CodeBadRequest, "The call's arguments cannot be sent."), err)
	}
	h := Headers{
		CallID:      strconv.FormatUint(features.NewTSID(), 10),
		Trace:       logging.TraceFrom(ctx),
		Deadline:    deadline,
		Meta:        a.Meta,
		Idempotency: o.key,
		Caller:      c.Caller,
		Tenant:      a.Principal.TenantID,
	}
	if a.Principal.Kind != "" {
		ref := a.Principal.Ref()
		h.Principal = &ref
	}
	msg := nats.NewMsg(Subject(service, op))
	h.Set(msg.Header)
	msg.Data = body

	rctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	retries := c.Retries
	if retries == 0 {
		retries = 3
	}
	for attempt := 0; ; attempt++ {
		reply, err := c.NC.RequestMsgWithContext(rctx, msg)
		switch {
		case err == nil:
			if derr := Decode(reply.Data, &resp); derr != nil || CommonOf(resp).Code == 0 {
				if derr == nil {
					derr = errors.New("the reply has no status")
				}
				return finish(Failed(http.StatusBadGateway, CodeBadReply, "The "+service+" service sent a reply that cannot be read."), derr)
			}
			return finish(features.ServiceResponse{}, nil)
		case errors.Is(err, nats.ErrNoResponders):
			if o.idempotent && retries > 0 && attempt < retries {
				wait := time.Duration(100<<attempt) * time.Millisecond
				if time.Now().Add(wait).Before(deadline) {
					select {
					case <-time.After(wait):
						continue
					case <-rctx.Done():
					}
				}
			}
			return finish(Unavailable(service), err)
		case errors.Is(err, context.Canceled):
			return finish(Failed(499, "call_canceled", "The call was canceled."), err)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
			return finish(Timeout(service), err)
		default:
			return finish(Unavailable(service), err)
		}
	}
}
