// Package services carries service calls over NATS (forge 21a-1,
// documentation/Services.md): a proxy implementation of a service's
// interface sends each call as a request on svc.<service>.<Op>, and a
// server, subscribed in the queue group svc.<service>, hands it to the real
// implementation and sends the response back. Callers cannot tell a proxy
// from the real implementation.
package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/nats-io/nats.go"
)

// Prefix is the first token of every service call's subject.
const Prefix = "svc"

// Headers carrying what ServiceArgs holds; the body is the operation's
// args without them (Encode).
const (
	HeaderCall        = "Svc-Call"
	HeaderTrace       = "Svc-Trace"
	HeaderDeadline    = "Svc-Deadline"
	HeaderMeta        = "Svc-Meta"
	HeaderIdempotency = "Svc-Idempotency"
	// HeaderPrincipal carries the principal in plain JSON. It is trusted
	// only by a server built with TrustHeaders (tests and loopback) until
	// the call token replaces it (forge 21b).
	HeaderPrincipal = "Svc-Principal"
	HeaderTenant    = "Svc-Tenant"
	// HeaderCaller names the calling process or service, for the logs.
	HeaderCaller = "Svc-Caller"
)

// Error codes a proxy or server answers with when the call itself, not the
// operation, failed.
const (
	CodeUnavailable = "service_unavailable" // 503: no server answered
	CodeTimeout     = "service_timeout"     // 504: the deadline passed
	CodeBadReply    = "service_bad_reply"   // 502: the reply did not decode
	CodeBadRequest  = "service_bad_request" // 400: the request did not decode
	CodeUnknownOp   = "service_unknown_op"  // 501: the server has no such operation
	CodeLocal       = "service_local"       // 501: only callable in the same process
	CodeUntrusted   = "call_untrusted"      // 401: who the call is for cannot be trusted
)

// Subject is the subject of op on service: svc.<service>.<op>. A routed
// tenant's calls (forge 21f-2) put a suffix before the op; servers read
// the op from the last token, so the suffix changes no server.
func Subject(service, op string) string {
	return Prefix + "." + service + "." + op
}

// Queue is the queue group every server of service subscribes in, so any
// number of processes share its calls with no leader.
func Queue(service string) string {
	return Prefix + "." + service
}

// OpOf is the operation a call's subject names: its last token.
func OpOf(subject string) string {
	return subject[strings.LastIndexByte(subject, '.')+1:]
}

// Headers are a call's headers.
type Headers struct {
	CallID      string
	Trace       string
	Deadline    time.Time
	Meta        features.Meta
	Idempotency string
	Principal   *features.PrincipalRef // nil when the call carries none
	Tenant      uint64
	Caller      string
	Token       string // the call token (forge 21b)
}

// Set writes the headers onto a request.
func (h Headers) Set(hdr nats.Header) {
	hdr.Set(HeaderCall, h.CallID)
	if h.Trace != "" {
		hdr.Set(HeaderTrace, h.Trace)
	}
	if !h.Deadline.IsZero() {
		hdr.Set(HeaderDeadline, h.Deadline.UTC().Format(time.RFC3339Nano))
	}
	if h.Meta != (features.Meta{}) {
		m, _ := json.Marshal(h.Meta)
		hdr.Set(HeaderMeta, string(m))
	}
	if h.Idempotency != "" {
		hdr.Set(HeaderIdempotency, h.Idempotency)
	}
	if h.Principal != nil {
		p, _ := json.Marshal(h.Principal)
		hdr.Set(HeaderPrincipal, string(p))
	}
	if h.Tenant != 0 {
		hdr.Set(HeaderTenant, strconv.FormatUint(h.Tenant, 10))
	}
	if h.Caller != "" {
		hdr.Set(HeaderCaller, h.Caller)
	}
	if h.Token != "" {
		hdr.Set(HeaderToken, h.Token)
	}
}

// ReadHeaders reads a call's headers.
func ReadHeaders(hdr nats.Header) (Headers, error) {
	h := Headers{CallID: hdr.Get(HeaderCall), Trace: hdr.Get(HeaderTrace), Idempotency: hdr.Get(HeaderIdempotency), Caller: hdr.Get(HeaderCaller), Token: hdr.Get(HeaderToken)}
	if h.CallID == "" {
		return h, fmt.Errorf("the call has no id")
	}
	if d := hdr.Get(HeaderDeadline); d != "" {
		t, err := time.Parse(time.RFC3339Nano, d)
		if err != nil {
			return h, fmt.Errorf("the call's deadline %q cannot be read", d)
		}
		h.Deadline = t
	}
	if m := hdr.Get(HeaderMeta); m != "" {
		if err := json.Unmarshal([]byte(m), &h.Meta); err != nil {
			return h, fmt.Errorf("the call's meta cannot be read: %v", err)
		}
	}
	if p := hdr.Get(HeaderPrincipal); p != "" {
		var ref features.PrincipalRef
		if err := json.Unmarshal([]byte(p), &ref); err != nil {
			return h, fmt.Errorf("the call's principal cannot be read: %v", err)
		}
		h.Principal = &ref
	}
	if t := hdr.Get(HeaderTenant); t != "" {
		n, err := strconv.ParseUint(t, 10, 64)
		if err != nil {
			return h, fmt.Errorf("the call's tenant %q cannot be read", t)
		}
		h.Tenant = n
	}
	return h, nil
}

// failure is a call's reply when the server could not run the operation (a
// request that did not decode, an unknown operation, an untrusted
// principal). A served operation's reply is its response itself, which has
// the same Common field.
type failure struct {
	Common features.ServiceResponse
}

// Failed is the response of a call that failed before or after its
// operation, with status and code.
func Failed(status uint16, code, message string) features.ServiceResponse {
	if message == "" {
		message = http.StatusText(int(status))
	}
	return features.ServiceResponse{Code: status, ErrorCode: code, Message: message}
}

// Unavailable is 503 service_unavailable naming service.
func Unavailable(service string) features.ServiceResponse {
	return Failed(http.StatusServiceUnavailable, CodeUnavailable, fmt.Sprintf("The %s service is unavailable.", service))
}

// Timeout is 504 service_timeout naming service.
func Timeout(service string) features.ServiceResponse {
	return Failed(http.StatusGatewayTimeout, CodeTimeout, fmt.Sprintf("The %s service did not answer in time.", service))
}
