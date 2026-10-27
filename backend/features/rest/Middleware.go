// Package rest holds what every feature's handlers share: the request
// middleware that gives each request its trace and principal, binding a
// request into ServiceArgs, and the status fields of every response DTO.
package rest

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/gin-gonic/gin"
)

// TraceHeader carries the trace ID: the browser sets it on every request and
// the server echoes it, making one when the request had none.
const TraceHeader = "X-Trace-Id"

const (
	principalKey = "foundation.principal"
	authErrKey   = "foundation.authError"
)

// Authenticate finds a request's principal; an error says why it is
// anonymous (identity.Authenticator.Authenticate).
type Authenticate func(r *http.Request) (features.Principal, error)

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middleware)

type middleware struct {
	authenticate Authenticate
}

// WithAuthenticate sets how requests find their principal. Without it every
// request runs as features.LocalPrincipal (single-user mode).
func WithAuthenticate(f Authenticate) MiddlewareOption {
	return func(m *middleware) {
		m.authenticate = f
	}
}

// validTrace keeps arbitrary header text out of the logs.
var validTrace = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// Middleware gives every request a trace ID and a principal, puts both in
// the request context for logging and errors, and logs one record per
// request in place of gin's logger. It never refuses a request: RequireUser
// does that for the routes that need a user.
func Middleware(opts ...MiddlewareOption) gin.HandlerFunc {
	m := middleware{authenticate: func(*http.Request) (features.Principal, error) { return features.LocalPrincipal(), nil }}
	for _, opt := range opts {
		opt(&m)
	}
	return func(c *gin.Context) {
		start := time.Now()
		trace := c.GetHeader(TraceHeader)
		if !validTrace.MatchString(trace) {
			trace = logging.NewTraceID()
		}
		c.Header(TraceHeader, trace)

		p, authErr := m.authenticate(c.Request)
		c.Set(principalKey, p)
		if authErr != nil {
			c.Set(authErrKey, authErr)
		}
		b := logging.Trace(trace)
		if p.UserID != 0 {
			b = b.User(strconv.FormatUint(p.UserID, 10))
		}
		if p.TenantID != 0 {
			b = b.Tenant(strconv.FormatUint(p.TenantID, 10))
		}
		ctx := logging.WithContext(c.Request.Context(), b)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelWarn // Fail has logged the error itself
		}
		logging.In("http").Tags(c.Request.Method).With(
			"method", c.Request.Method, "route", route, "status", status,
			"durationMs", time.Since(start).Milliseconds(), "bytes", max(c.Writer.Size(), 0),
		).Log(ctx, level, nil, "%s %s %d", c.Request.Method, route, status)
	}
}

// Args is the ServiceArgs of the request: its context and principal.
func Args(c *gin.Context) features.ServiceArgs {
	p, ok := c.Get(principalKey)
	if !ok {
		p = features.LocalPrincipal() // a router without Middleware, as in tests
	}
	return features.ServiceArgs{Ctx: c.Request.Context(), Principal: p.(features.Principal)}
}

// ErrExpired is what an Authenticate returns for an expired session, which
// RequireUser answers with session_expired so the browser refreshes it.
var ErrExpired = errors.New("rest: the session has expired")

// RequireUser refuses anonymous requests with 401: session_expired when the
// session has expired (the browser refreshes it and tries again), and
// unauthenticated otherwise.
func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		args := Args(c)
		if args.Principal.Kind != features.PrincipalAnonymous {
			c.Next()
			return
		}
		code, message := "unauthenticated", "Sign in first"
		if err, _ := c.Get(authErrKey); err != nil && errors.Is(err.(error), ErrExpired) {
			code, message = "session_expired", "Your session has expired"
		}
		failed := args.Fail(http.StatusUnauthorized, logging.In("rest").Code(code).Public(message).New(message))
		c.AbortWithStatusJSON(http.StatusUnauthorized, StatusFrom(*failed))
	}
}
