package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// Event sources (forge 07c). Foundation lists in cx_sources the
// connections whose events a trigger listens to. Every instance serving a
// type contends for each of its sources' lease in cx_leases (created only
// if absent, refreshed with the revision it holds, gone after LeaseTTL);
// the holder runs the type's Watch, publishes each event to CX_<tenant>
// with the event's id as its message id, then stores its cursor. A holder
// that cannot refresh stops its Watch before the lease ends, so two never
// run at once; one that dies is replaced by any instance, in any process,
// which resumes from the cursor.

// DefaultPollInterval is how long a poll source waits between passes when
// its connection names no interval.
var DefaultPollInterval = time.Minute

// held is a source this instance runs.
type held struct {
	src    connectors.Source
	rev    uint64 // the lease's
	since  time.Time
	lastOK time.Time
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	lastErr string
	errAt   time.Time
}

func (h *held) setError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		h.lastErr, h.errAt = "", time.Time{}
		return
	}
	h.lastErr, h.errAt = err.Error(), time.Now().UTC()
}

type sourcesRun struct {
	s     *server
	mu    sync.Mutex
	known map[string]connectors.Source // by key, of this instance's types and tenants
	held  map[string]*held
}

// runSources contends for and runs event sources until ctx ends.
func (s *server) runSources(ctx context.Context) {
	if !s.watches() {
		return
	}
	r := &sourcesRun{s: s, known: map[string]connectors.Source{}, held: map[string]*held{}}
	updates, err := s.books.Sources(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Error(ctx, err, "adapter %s: cannot watch the event sources", s.o.Name)
		}
		return
	}
	tick := time.NewTicker(s.o.LeaseEvery)
	defer tick.Stop()
	defer r.stopAll()
	for {
		select {
		case <-ctx.Done():
			return
		case u, ok := <-updates:
			if !ok {
				return
			}
			if u.Ready {
				r.step(ctx) // the current entries are in
				continue
			}
			r.seen(u)
			r.step(ctx)
		case <-tick.C:
			r.step(ctx)
		}
	}
}

// watches reports whether a type this adapter serves has watched events.
func (s *server) watches() bool {
	for _, t := range s.types {
		for _, ref := range t.Contracts {
			if c, ok := s.refs[ref]; ok {
				for _, e := range c.Events {
					if slices.Contains(e.Delivery, connectors.DeliveryWatch) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (r *sourcesRun) seen(u connectors.SourceUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u.Source == nil {
		delete(r.known, u.Key)
		return
	}
	src := *u.Source
	if _, ok := r.s.impls[src.Type]; !ok || (len(r.s.o.Tenants) > 0 && !slices.Contains(r.s.o.Tenants, src.Tenant)) {
		delete(r.known, u.Key)
		return
	}
	r.known[u.Key] = src
}

// step refreshes this instance's leases, stops the sources that ended or
// changed, and takes free ones up to its share.
func (r *sourcesRun) step(ctx context.Context) {
	now := time.Now()
	r.mu.Lock()
	keys := make([]string, 0, len(r.held))
	for k := range r.held {
		keys = append(keys, k)
	}
	r.mu.Unlock()
	for _, k := range keys {
		r.mu.Lock()
		h := r.held[k]
		src, listed := r.known[k]
		r.mu.Unlock()
		if !listed || src.Revision != h.src.Revision || src.PollInterval != h.src.PollInterval || src.Since != h.src.Since {
			r.stop(k, true)
			continue
		}
		rev, err := r.s.books.LeaseUpdate(ctx, k, r.lease(h), h.rev)
		if err == nil {
			h.rev, h.lastOK = rev, now
			continue
		}
		// Not refreshed: stop before the lease can end and another take it
		if errors.Is(err, connectors.ErrLeaseLost) || now.Sub(h.lastOK) >= connectors.LeaseTTL-r.s.o.LeaseEvery {
			log.Warn(ctx, "adapter %s: lost the lease on %s: %v", r.s.o.Name, k, err)
			r.stop(k, false)
		}
	}
	// Take free sources while this instance holds fewer than its share
	share := r.shares(ctx)
	r.mu.Lock()
	var free []string
	count := map[string]int{}
	for _, h := range r.held {
		count[h.src.Type]++
	}
	for k := range r.known {
		if r.held[k] == nil {
			free = append(free, k)
		}
	}
	r.mu.Unlock()
	sort.Strings(free)
	for _, k := range free {
		r.mu.Lock()
		src := r.known[k]
		r.mu.Unlock()
		if count[src.Type] >= share[src.Type] {
			continue
		}
		h := &held{src: src, since: now, lastOK: now}
		rev, err := r.s.books.LeaseCreate(ctx, k, r.lease(h))
		if err != nil {
			continue // another instance holds it
		}
		h.rev = rev
		count[src.Type]++
		r.start(k, h)
	}
}

// shares are, by type, how many sources this instance may hold: its share
// of the type's sources among the live instances serving it, plus one
// (07c question 3). Running sources are never handed over.
func (r *sourcesRun) shares(ctx context.Context) map[string]int {
	sources := map[string]int{}
	r.mu.Lock()
	for _, src := range r.known {
		sources[src.Type]++
	}
	r.mu.Unlock()
	instances := map[string]int{}
	if all, err := r.s.books.Announcements(ctx); err == nil {
		now := time.Now()
		for _, a := range all {
			if a.Online(now) && a.Type != nil {
				instances[a.Type.ID]++
			}
		}
	}
	out := map[string]int{}
	for typ, n := range sources {
		i := max(instances[typ], 1)
		out[typ] = n/i + 1
	}
	return out
}

func (r *sourcesRun) lease(h *held) connectors.Lease {
	h.mu.Lock()
	defer h.mu.Unlock()
	return connectors.Lease{Adapter: r.s.o.Name, Instance: r.s.instance, Host: r.s.o.Host, Since: h.since.UTC(), LastError: h.lastErr, LastErrorAt: h.errAt}
}

func (r *sourcesRun) start(key string, h *held) {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan struct{})
	r.mu.Lock()
	r.held[key] = h
	r.mu.Unlock()
	go func() {
		defer close(h.done)
		r.s.runSource(ctx, h)
	}()
}

// stop stops a source this instance runs; release gives its lease back at
// once, rather than when it ends.
func (r *sourcesRun) stop(key string, release bool) {
	r.mu.Lock()
	h := r.held[key]
	delete(r.held, key)
	r.mu.Unlock()
	if h == nil {
		return
	}
	h.cancel()
	<-h.done
	if release {
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.s.books.LeaseDelete(bg, key, h.rev)
	}
}

func (r *sourcesRun) stopAll() {
	r.mu.Lock()
	keys := make([]string, 0, len(r.held))
	for k := range r.held {
		keys = append(keys, k)
	}
	r.mu.Unlock()
	for _, k := range keys {
		r.stop(k, true)
	}
}

// runSource runs one source's Watch until ctx ends: again after the poll
// interval when it returns, after a back-off when it fails. An auth error
// is reported to Foundation, which marks the connection and lists the
// source no more.
func (s *server) runSource(ctx context.Context, h *held) {
	src := h.src
	interval := DefaultPollInterval
	if d, err := time.ParseDuration(src.PollInterval); err == nil && d > 0 {
		interval = d
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.pass(ctx, h)
		if ctx.Err() != nil {
			return
		}
		wait := interval
		if err != nil {
			h.setError(err)
			e := connectors.AsError(err)
			log.With("connection", src.Connection, "type", src.Type).Debug(ctx, "event source %s failed: %s", src.Connection, e.Code)
			if e.Code == connectors.CodeAuth {
				s.reportStatus(ctx, src, e)
			}
			wait = min(backoff, interval)
			backoff = min(backoff*2, time.Minute)
		} else {
			h.setError(nil)
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *server) reportStatus(ctx context.Context, src connectors.Source, e *connectors.Error) {
	body, _ := json.Marshal(connectors.StatusReport{Error: *e})
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.nc.RequestWithContext(rctx, connectors.StatusSubject(src.Tenant, src.Connection), body); err != nil {
		log.Warn(ctx, "the auth error of connection %s could not be reported: %v", src.Connection, err)
	}
}

// pass opens a session on the source's connection and runs its Watch once:
// until cancelled for a push connector, one pass for a poll connector.
func (s *server) pass(ctx context.Context, h *held) error {
	src := h.src
	im := s.impls[src.Type]
	key := connectors.SourceKey(src.Tenant, src.Connection)
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(src.Tenant)}
	b := log.With("type", src.Type, "connection", src.Connection)
	x := Context{Context: ctx, Args: args, Connection: src.Connection, Log: b, books: s.books}
	if src.Since != "" {
		if t, err := time.Parse("2006-01-02", src.Since); err == nil {
			x.Since = t
		} else if t, err := time.Parse(time.RFC3339, src.Since); err == nil {
			x.Since = t
		}
	}
	if s.types[src.Type].Exclusive {
		release, err := s.books.Lock(ctx, src.Tenant, src.Connection, s.instance)
		if err != nil {
			return err
		}
		defer release()
	}
	conn, err := s.resolve(ctx, src.Tenant, src.Type, src.Connection)
	if err != nil {
		return err
	}
	sess, err := im.Open(x, conn)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	w, ok := sess.(Watcher)
	if !ok {
		return connectors.Errorf(connectors.CodeNotSupported, "%s has no event source", src.Type)
	}
	var cursor json.RawMessage
	if c, ok, err := s.books.Cursor(ctx, key); err != nil {
		return err
	} else if ok {
		cursor = c.Cursor
	}
	return w.Watch(x, cursor, func(e Event) error {
		if err := s.publish(ctx, src, e); err != nil {
			return err
		}
		if e.Cursor == nil {
			return nil
		}
		return s.books.PutCursor(ctx, key, connectors.Cursor{Cursor: e.Cursor, At: time.Now().UTC(), LastEvent: e.Name + ":" + e.ID})
	})
}

// publish puts one event in CX_<tenant> (connectors.PublishEvent), after
// checking the type implements its contract.
func (s *server) publish(ctx context.Context, src connectors.Source, e Event) error {
	if !slices.Contains(s.types[src.Type].Contracts, e.Contract) {
		return connectors.Errorf(connectors.CodeInvalid, "%s does not implement %s", src.Type, e.Contract)
	}
	return s.books.Publish(ctx, src.Tenant, src.Connection, e.Contract, e.Name, e.ID, e.Payload, e.At)
}
