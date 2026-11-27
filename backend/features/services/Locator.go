package services

import (
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Locator hands each caller a service as the process's service set has it
// (forge 21f-1): the real implementation when the set runs the service,
// else its proxy. With Loopback every service is real and served, and
// every caller gets the proxy, so every call crosses NATS in one process.
// A nil Locator, or one with no set, runs every service, as serve always
// did, with no server and no NATS hop.
type Locator struct {
	// Client sends the proxies' calls.
	Client *Client
	// Set is the services this process runs, by wire name; nil runs all.
	Set map[string]bool
	// Serve starts a server for each service the process runs, so other
	// processes may call it (serve --services, 21g-1).
	Serve bool
	// Loopback serves every service and gives every caller the proxy.
	Loopback bool
	// ServerOptions are the servers' (VerifyWith, Concurrency).
	ServerOptions []ServerOption

	mu      sync.Mutex
	got     map[string]any
	servers []*Server
	failed  []error
	// proxied and served are the services handed out as proxies and the
	// ones served (forge 21g-1: readiness waits for the proxied, the
	// announcement names the served).
	proxied, served []string
}

// Runs reports whether the process runs a service.
func (l *Locator) Runs(name string) bool {
	return l == nil || l.Set == nil || l.Set[name] || l.Loopback
}

// Use is the service named name as the locator gives it: real (made at
// most once) when the process runs it, served when it serves, the proxy
// otherwise or in loopback. The generated proxy package's Use calls it.
func Use[S any](l *Locator, name string, real func() S, proxy func(c *Client, local S) S, serve func(*nats.Conn, S, ...ServerOption) (*Server, error)) S {
	if l == nil {
		return real()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if got, ok := l.got[name]; ok {
		return got.(S)
	}
	if l.got == nil {
		l.got = map[string]any{}
	}
	var out S
	connected := l.Client != nil && l.Client.NC != nil
	switch {
	case !connected:
		// Before NATS is up (the bootstrap routes) every service is real
		out = real()
	case !l.Runs(name):
		var none S
		out = proxy(l.Client, none)
		l.proxied = append(l.proxied, name)
	case l.Loopback || l.Serve:
		r := real()
		srv, err := serve(l.Client.NC, r, l.ServerOptions...)
		if err != nil {
			l.failed = append(l.failed, err)
		} else {
			l.servers = append(l.servers, srv)
			l.served = append(l.served, name)
		}
		out = r
		if l.Loopback {
			out = proxy(l.Client, r)
		}
	default:
		out = real()
	}
	l.got[name] = out
	return out
}

// Get is a service Use already gave; ok is false before.
func Get[S any](l *Locator, name string) (s S, ok bool) {
	if l == nil {
		return s, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	got, ok := l.got[name]
	if !ok {
		return s, false
	}
	s, ok = got.(S)
	return s, ok
}

// Failed are the servers that could not start.
func (l *Locator) Failed() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.failed...)
}

// Proxied are the services this process calls in other processes.
func (l *Locator) Proxied() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.proxied...)
}

// Served are the services this process serves to others.
func (l *Locator) Served() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.served...)
}

// Stop stops the servers the locator started, waiting for the calls in
// flight.
func (l *Locator) Stop() { l.StopWithin(0) }

// StopWithin drains: every server leaves its queue group at once, so new
// calls go to other processes, and the calls in flight get up to d to
// finish (0: as long as they take). It reports whether they all did.
func (l *Locator) StopWithin(d time.Duration) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	servers := l.servers
	l.servers = nil
	l.mu.Unlock()
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for _, s := range servers {
			wg.Add(1)
			go func() { defer wg.Done(); s.Stop() }()
		}
		wg.Wait()
		close(done)
	}()
	if d <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
