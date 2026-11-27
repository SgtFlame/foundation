package services

import (
	"strconv"
	"sync"
)

// CodeMoved is the refusal of a server whose tenant's calls are routed
// elsewhere: the caller sends the call again where its routes now say.
const CodeMoved = "service_moved" // 503

// Routes are where each tenant's calls of a service go (forge 21f-2): a
// routed tenant's calls go to svc.<service>.<suffix>.<op>, served by the
// servers started for that suffix (ForSuffix), and every other call to
// svc.<service>.<op>. The services/routes package keeps them current from
// KV svc_routes; the tenant is the call's principal's, which its token
// carries, never an argument.
type Routes struct {
	mu sync.RWMutex
	m  map[string]string
}

// NewRoutes is an empty table: every call goes to the shared servers.
func NewRoutes() *Routes { return &Routes{m: map[string]string{}} }

// RouteKey is a tenant's route of service: <service>.t<tenant>.
func RouteKey(service string, tenant uint64) string {
	return service + ".t" + strconv.FormatUint(tenant, 10)
}

// Set routes tenant's calls of service to suffix; an empty suffix sends
// them back to the shared servers.
func (r *Routes) Set(service string, tenant uint64, suffix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if suffix == "" {
		delete(r.m, RouteKey(service, tenant))
		return
	}
	r.m[RouteKey(service, tenant)] = SubjectToken(suffix)
}

// Suffix is where tenant's calls of service go: "" for the shared servers.
// A nil table routes nothing.
func (r *Routes) Suffix(service string, tenant uint64) string {
	if r == nil || tenant == 0 {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[RouteKey(service, tenant)]
}

// RoutedSubject is op of service for tenant as r routes it.
func RoutedSubject(r *Routes, service, op string, tenant uint64) string {
	if s := r.Suffix(service, tenant); s != "" {
		return Prefix + "." + service + "." + s + "." + op
	}
	return Subject(service, op)
}

// ForSuffix makes a server serve only the calls routed to suffix (a
// dedicated server, such as one per tenant), in its own queue group.
func ForSuffix(suffix string) ServerOption {
	return func(s *Server) { s.suffix = SubjectToken(suffix) }
}

// WithRoutes makes a server refuse, with 503 service_moved, a call whose
// tenant routes now send elsewhere, so moving a tenant drains the old
// servers' calls for it while the callers resend them.
func WithRoutes(r *Routes) ServerOption { return func(s *Server) { s.routes = r } }
