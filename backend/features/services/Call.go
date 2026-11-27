package services

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
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
	// Keys, in a process that runs an entry point, sign the call token of
	// a call whose context carries none, or one for another principal
	// (forge 21b). A process without them only forwards the tokens its
	// calls came with.
	Keys *Keys
	// Retries is how many times an idempotent call is sent again when no
	// server answers (3 when 0; -1 for none).
	Retries int
	// Routes send a routed tenant's calls to its dedicated servers (forge
	// 21f-2); nil sends every call to the shared ones.
	Routes *Routes
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
	return call[R](c, "", service, op, args, opts)
}

// CallAt is Call sent to subject: a WaitSubject, where one process waits
// for it (forge 21c).
func CallAt[R any](c *Client, subject, service string, args any, opts ...CallOption) R {
	return call[R](c, subject, service, OpOf(subject), args, opts)
}

func call[R any](c *Client, subject, service, op string, args any, opts []CallOption) R {
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

	msg, err := c.request(ctx, service, op, args, opts, false)
	if err == nil && subject != "" {
		msg.Subject = subject
	}
	if err != nil {
		var ce *callError
		if errors.As(err, &ce) {
			return finish(ce.resp, ce.err)
		}
		return finish(Failed(http.StatusBadRequest, CodeBadRequest, ""), err)
	}
	deadline, _ := ReadDeadline(msg)
	callID := msg.Header.Get(HeaderCall)
	rctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	retries := c.Retries
	if retries == 0 {
		retries = 3
	}
	moved := 0
	for attempt := 0; ; attempt++ {
		reply, err := c.NC.RequestMsgWithContext(rctx, msg)
		switch {
		case err == nil:
			var fresh R
			resp = fresh
			if derr := Decode(reply.Data, &resp); derr != nil || CommonOf(resp).Code == 0 {
				if derr == nil {
					derr = errors.New("the reply has no status")
				}
				return finish(Failed(http.StatusBadGateway, CodeBadReply, "The "+service+" service sent a reply that cannot be read."), derr)
			}
			// A server whose tenant moved refused the call before running
			// it: send it again where the routes now say, once they do
			if CommonOf(resp).ErrorCode == CodeMoved && subject == "" && moved < 8 {
				moved++
				select {
				case <-time.After(time.Duration(25<<min(moved, 5)) * time.Millisecond):
					msg.Subject = RoutedSubject(c.Routes, service, op, a.Principal.TenantID)
					attempt--
					continue
				case <-rctx.Done():
					return finish(Timeout(service), rctx.Err())
				}
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
			// Tell the server holding the call to stop it (forge 21c).
			_ = c.NC.Publish(CancelSubject(service, callID), nil)
			return finish(Failed(499, "call_canceled", "The call was canceled."), err)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
			return finish(Timeout(service), err)
		default:
			return finish(Unavailable(service), err)
		}
	}
}

// callError is a call that failed before it was sent, with its response.
type callError struct {
	resp features.ServiceResponse
	err  error
}

func (e *callError) Error() string { return e.err.Error() }

// ReadDeadline is the deadline a call's message carries.
func ReadDeadline(m *nats.Msg) (time.Time, bool) {
	h, err := ReadHeaders(m.Header)
	if err != nil || h.Deadline.IsZero() {
		return time.Time{}, false
	}
	return h.Deadline, true
}

// request is the message for op of service with args: body, headers and
// call token. A plain call always has a deadline (the context's, else the
// client's timeout); a stream has one only when its context does.
func (c *Client) request(ctx context.Context, service, op string, args any, opts []CallOption, stream bool) (*nats.Msg, error) {
	var o callOptions
	for _, opt := range opts {
		opt(&o)
	}
	a := ArgsOf(args)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	deadline, ok := ctx.Deadline()
	if !ok && !stream {
		deadline = time.Now().Add(timeout)
	}
	body, err := Encode(args)
	if err != nil {
		return nil, &callError{Failed(http.StatusBadRequest, CodeBadRequest, "The call's arguments cannot be sent."), err}
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
	if authz.HasAs(ctx) {
		return nil, &callError{Failed(http.StatusForbidden, CodeAsLocal, "A check decided by a role cannot be sent to another process."), errors.New("the call runs under authz.WithAs")}
	}
	if a.Principal.Kind != "" {
		tok, has := TokenFrom(ctx)
		fresh := has && time.Until(time.Unix(tok.Expires, 0)) > 30*time.Second
		switch {
		case has && tok.Names(a.Principal) && (fresh || c.Keys == nil):
			h.Token = tok.Raw()
		case c.Keys != nil:
			var parent *Token
			if has && !tok.Names(a.Principal) {
				parent = &tok
			}
			t, err := c.Keys.Mint(a.Principal, parent)
			if err != nil {
				return nil, &callError{Failed(http.StatusInternalServerError, "call_token", ""), err}
			}
			h.Token = t.Raw()
		case has:
			return nil, &callError{Failed(http.StatusForbidden, CodePrincipalChanged, "This process cannot make a call for another principal."), errors.New("no call key to narrow the token")}
		default:
			ref := a.Principal.Ref()
			h.Principal = &ref
		}
	}
	msg := nats.NewMsg(RoutedSubject(c.Routes, service, op, a.Principal.TenantID))
	h.Set(msg.Header)
	msg.Data = body
	return msg, nil
}
