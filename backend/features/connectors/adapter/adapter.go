// Package adapter is the connector adapter runtime (forge 07b,
// documentation/Connectors.md): a process component that serves connector
// types on the connector bus. It answers their call requests in a queue
// group per type, opens and keeps their sessions, and announces what it
// serves in KV cx_adapters. The same code runs in the server's process,
// on its own NATS connection, and in an external binary connected to the
// same NATS cluster, so a type moves between them without change.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

var log = logging.In("connectors").Tags("adapter")

// Connection is what a session is opened with: the connection, its type
// and its settings, secrets included (07c resolves them; an adapter never
// logs or sends them). A type that needs no connection gets an empty one.
type Connection struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Tenant   uint64          `json:"tenant,string"`
	Settings json.RawMessage `json:"-"` // never marshalled
}

// Context is what a type's code sees of a call or a session: the
// caller's ServiceArgs (principal, Meta, trace) and a logger that never
// logs settings. Everything it gives reaches JetStream over the adapter's
// own NATS connection, so it works the same in an external process. The
// tenant's file store and the ledger writer join it with 07c.
type Context struct {
	context.Context
	Args      features.ServiceArgs
	CallID    string
	Contract  string // id@version
	Operation string
	Key       string // the call's idempotency key
	Log       logging.Builder
	js        jetstream.JetStream
}

// Tenant is the call's tenant.
func (c Context) Tenant() uint64 { return c.Args.Principal.TenantID }

// PutDiscovered stores the contract a connection discovered (15c).
func (c Context) PutDiscovered(connection string, contract connectors.Contract) (connectors.Discovered, error) {
	return connectors.PutDiscovered(c, c.js, c.Tenant(), connection, contract)
}

// Impl is a type's implementation.
type Impl struct {
	Type string
	// Open opens a session on a connection; the runtime keeps it until the
	// connection changes or the adapter stops.
	Open func(ctx Context, conn Connection) (Session, error)
	// Receive, for a type with Receives, verifies a callback and turns it
	// into events (15f; declared, not called in 07b).
	Receive Receiver
	// Register registers the hook URL with the service (15f; not called).
	Register Registrar
}

// Session is an open connection to the outside service.
type Session interface {
	// Call runs one operation. A *connectors.Error is replied as it is;
	// any other error is unavailable.
	Call(ctx Context, op string, args json.RawMessage) (json.RawMessage, error)
	Close() error
}

// Event is a connector event, as Watch emits it and Receive returns it.
type Event struct {
	Contract string          `json:"contract"` // id@version
	Name     string          `json:"name"`
	ID       string          `json:"id"` // from the event's id fields
	Payload  json.RawMessage `json:"payload"`
	At       time.Time       `json:"at"`
}

// Watcher is optional, for a session whose contracts declare watched
// events (07c runs it under a lease, with a cursor).
type Watcher interface {
	Watch(ctx Context, cursor json.RawMessage, emit func(Event) error) error
}

// Describer is optional, for a type that discovers its contract (15c,
// 15e): called when the connection opens and when the service says its
// list changed.
type Describer interface {
	Describe(ctx Context) (connectors.Contract, error)
}

// Receiver verifies a callback with the connection's secret and turns it
// into events (15f). An error refuses it (4xx); the runtime publishes the
// events, then replies 2xx.
type Receiver interface {
	Receive(ctx Context, conn Connection, cb Callback) ([]Event, error)
}

// Registrar registers the hook URL with the service itself (15f).
type Registrar interface {
	Register(ctx Context, conn Connection, url string, events []string) (Registration, error)
	Unregister(ctx Context, conn Connection, reg Registration) error
}

// Registration is what a Registrar keeps of a registered hook.
type Registration struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Callback is one HTTP request a service pushed to a hook.
type Callback struct {
	Method, Path string
	Header       http.Header
	Body         []byte
	ReceivedAt   time.Time
}

// Surface is an inbound surface (15a): it listens on an outside endpoint
// and calls the gateway on the bus, never Axiom's code in memory.
type Surface struct {
	ID     string // mcp.server, http.tools
	Meta   connectors.Meta
	Listen func(ctx context.Context, gw Gateway) error // returns when ctx ends
}

// Credential is who a surface's caller is (a client key, 15a).
type Credential struct {
	Kind  string `json:"kind"`
	Token string `json:"-"`
}

// Gateway is the bus client a surface uses, implemented over cx.in (15a).
type Gateway interface {
	Request(ctx context.Context, cred Credential, exposure, method string, body json.RawMessage) (json.RawMessage, error)
	Notify(ctx context.Context, session string, fn func(json.RawMessage)) (stop func(), err error)
}

// noGateway is the gateway until 15a: every request is not supported, and
// nothing is sent on cx.in.
type noGateway struct{}

func (noGateway) Request(context.Context, Credential, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, connectors.Errorf(connectors.CodeNotSupported, "the gateway is not built yet (iteration 15)")
}

func (noGateway) Notify(context.Context, string, func(json.RawMessage)) (func(), error) {
	return nil, connectors.Errorf(connectors.CodeNotSupported, "the gateway is not built yet (iteration 15)")
}

// Options are what an adapter serves.
type Options struct {
	// Name names the adapter in its announcements (forge, mail-adapter).
	Name string
	// Types are the metadata of the types it serves, and Contracts the
	// contracts they name (announced with them).
	Types     []connectors.Type
	Contracts []connectors.Contract
	Impls     []Impl
	Surfaces  []Surface
	// Tenants it serves; every tenant when empty.
	Tenants []uint64
	// Resolve gives a connection's settings to open a session (07c's
	// cx.resolve); without it a session opens with none.
	Resolve func(ctx context.Context, tenant uint64, typ, connection string) (Connection, error)
	// AnnounceEvery is how often announcements are refreshed (5s), and
	// TTL how long one stays online unrefreshed (three times that).
	AnnounceEvery time.Duration
	TTL           time.Duration
	// MaxConcurrent is how many calls run at once (64).
	MaxConcurrent int
}

// Serve serves opts on nc until ctx ends, then stops taking calls, drops
// its announcements and closes its sessions. It returns at once when the
// options are wrong.
func Serve(ctx context.Context, nc *nats.Conn, opts Options) error {
	s, err := newServer(nc, opts)
	if err != nil {
		return err
	}
	return s.run(ctx)
}

type server struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	o        Options
	instance string
	impls    map[string]Impl
	types    map[string]connectors.Type
	refs     map[string]connectors.Contract
	sem      chan struct{}
	wg       sync.WaitGroup

	mu       sync.Mutex
	sessions map[string]*entry // tenant/type/connection
}

type entry struct {
	mu   sync.Mutex
	sess Session
}

func newServer(nc *nats.Conn, o Options) (*server, error) {
	if o.Name == "" {
		o.Name = "adapter"
	}
	if o.AnnounceEvery <= 0 {
		o.AnnounceEvery = 5 * time.Second
	}
	if o.TTL <= 0 {
		o.TTL = 3 * o.AnnounceEvery
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = 64
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	s := &server{nc: nc, js: js, o: o, instance: nuid.Next(), impls: map[string]Impl{}, types: map[string]connectors.Type{},
		refs: map[string]connectors.Contract{}, sem: make(chan struct{}, o.MaxConcurrent), sessions: map[string]*entry{}}
	for _, c := range o.Contracts {
		s.refs[c.Ref()] = c
	}
	for _, t := range o.Types {
		if t.Source == "" {
			t.Source = connectors.SourceBuiltin
		}
		s.types[t.ID] = t
	}
	for _, im := range o.Impls {
		if _, ok := s.types[im.Type]; !ok {
			return nil, fmt.Errorf("adapter %s implements %s, which its options do not describe", o.Name, im.Type)
		}
		if im.Open == nil {
			return nil, fmt.Errorf("adapter %s: %s has no Open", o.Name, im.Type)
		}
		s.impls[im.Type] = im
	}
	for id := range s.types {
		if _, ok := s.impls[id]; !ok {
			return nil, fmt.Errorf("adapter %s describes %s but does not implement it", o.Name, id)
		}
	}
	return s, nil
}

func (s *server) run(ctx context.Context) error {
	var subs []*nats.Subscription
	for id := range s.impls {
		subjects := []string{connectors.CallSubjects(id)}
		if len(s.o.Tenants) > 0 {
			subjects = nil
			for _, t := range s.o.Tenants {
				subjects = append(subjects, connectors.TenantCallSubjects(t, id))
			}
		}
		for _, subj := range subjects {
			sub, err := s.nc.QueueSubscribe(subj, connectors.CallQueue(id), s.receive)
			if err != nil {
				s.unsubscribe(subs)
				return err
			}
			subs = append(subs, sub)
		}
	}
	changed, err := s.nc.Subscribe(connectors.ChangedSubjects, func(m *nats.Msg) {
		parts := strings.Split(m.Subject, ".")
		if len(parts) == 4 {
			s.drop(parts[2], parts[3])
		}
	})
	if err != nil {
		s.unsubscribe(subs)
		return err
	}
	subs = append(subs, changed)
	if err := s.nc.Flush(); err != nil {
		s.unsubscribe(subs)
		return err
	}
	kv, err := connectors.AdaptersKV(ctx, s.js)
	if err != nil {
		s.unsubscribe(subs)
		return err
	}
	// Surfaces listen with a gateway that refuses every request until 15a
	surfaces, stopSurfaces := context.WithCancel(ctx)
	for _, sf := range s.o.Surfaces {
		if sf.Listen == nil {
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := sf.Listen(surfaces, noGateway{}); err != nil && surfaces.Err() == nil {
				log.Error(ctx, err, "surface %s stopped", sf.ID)
			}
		}()
	}
	s.announce(ctx, kv)
	tick := time.NewTicker(s.o.AnnounceEvery)
	defer tick.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-tick.C:
			s.announce(ctx, kv)
		}
	}
	stopSurfaces()
	s.unsubscribe(subs)
	s.wg.Wait()
	// Gone at once rather than when the announcements expire
	bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, a := range s.announcements() {
		_ = kv.Delete(bg, a.Key())
	}
	s.mu.Lock()
	for k, e := range s.sessions {
		if e.sess != nil {
			_ = e.sess.Close()
		}
		delete(s.sessions, k)
	}
	s.mu.Unlock()
	return nil
}

func (s *server) unsubscribe(subs []*nats.Subscription) {
	for _, sub := range subs {
		_ = sub.Unsubscribe()
	}
}

// announcements are this instance's: one per type, one per surface.
func (s *server) announcements() []connectors.Announcement {
	now := time.Now().UTC()
	var out []connectors.Announcement
	for _, t := range s.types {
		var contracts []connectors.Contract
		for _, ref := range t.Contracts {
			if c, ok := s.refs[ref]; ok {
				contracts = append(contracts, c)
			}
		}
		out = append(out, connectors.Announcement{Adapter: s.o.Name, Instance: s.instance, Type: &t, Contracts: contracts,
			Tenants: s.o.Tenants, At: now, ExpiresAt: now.Add(s.o.TTL)})
	}
	for _, sf := range s.o.Surfaces {
		out = append(out, connectors.Announcement{Adapter: s.o.Name, Instance: s.instance, Surface: &connectors.SurfaceInfo{ID: sf.ID, Meta: sf.Meta},
			Tenants: s.o.Tenants, At: now, ExpiresAt: now.Add(s.o.TTL)})
	}
	return out
}

func (s *server) announce(ctx context.Context, kv jetstream.KeyValue) {
	for _, a := range s.announcements() {
		if err := connectors.Announce(ctx, kv, a); err != nil && ctx.Err() == nil {
			log.Warn(ctx, "announcing %s failed: %v", a.Key(), err)
		}
	}
}

// receive takes a call from the bus; it runs beside the others, at most
// MaxConcurrent at once.
func (s *server) receive(m *nats.Msg) {
	s.wg.Add(1)
	s.sem <- struct{}{}
	go func() {
		defer func() { <-s.sem; s.wg.Done() }()
		s.handle(m)
	}()
}

func (s *server) reply(m *nats.Msg, result json.RawMessage, e *connectors.Error) {
	raw, _ := json.Marshal(connectors.Reply{Result: result, Error: e})
	_ = m.Respond(raw)
}

func (s *server) handle(m *nats.Msg) {
	call, err := connectors.ParseCallSubject(m.Subject)
	if err != nil {
		s.reply(m, nil, connectors.Errorf(connectors.CodeInvalid, "%v", err))
		return
	}
	h, err := connectors.ReadCallHeaders(m.Header)
	if err != nil {
		s.reply(m, nil, connectors.Errorf(connectors.CodeInvalid, "%v", err))
		return
	}
	im, ok := s.impls[call.Type]
	if !ok {
		s.reply(m, nil, connectors.Errorf(connectors.CodeNotSupported, "this adapter does not serve %s", call.Type))
		return
	}
	t := s.types[call.Type]
	discovered := strings.HasPrefix(h.Contract, connectors.DiscoveredPrefix)
	if !discovered && !slices.Contains(t.Contracts, h.Contract) {
		s.reply(m, nil, connectors.Errorf(connectors.CodeNotSupported, "%s does not implement %s", call.Type, h.Contract))
		return
	}
	if !h.Deadline.IsZero() && time.Now().After(h.Deadline) {
		return // the caller stopped waiting
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if !h.Deadline.IsZero() {
		ctx, cancel = context.WithDeadline(ctx, h.Deadline)
	} else {
		ctx, cancel = context.WithTimeout(ctx, connectors.DefaultTimeout)
	}
	defer cancel()
	b := log.With("type", call.Type, "operation", call.Operation, "callId", h.CallID)
	if h.Trace != "" {
		b = b.Trace(h.Trace)
	}
	ctx = logging.WithContext(ctx, b)
	args := features.ServiceArgs{Ctx: ctx, Principal: h.Principal.In(call.Tenant), Meta: h.Meta}
	x := Context{Context: ctx, Args: args, CallID: h.CallID, Contract: h.Contract, Operation: call.Operation, Key: h.Key, Log: b, js: s.js}
	result, e := s.call(x, im, call, m.Data)
	if e != nil {
		b.Debug(ctx, "%s %s failed: %s", call.Type, call.Operation, e.Code)
	}
	s.reply(m, result, e)
}

// call runs a call on its connection's session, opening one first.
func (s *server) call(x Context, im Impl, call connectors.ParsedCall, data []byte) (result json.RawMessage, e *connectors.Error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error(x, fmt.Errorf("%v", r), "%s %s panicked", call.Type, call.Operation)
			result, e = nil, connectors.Errorf(connectors.CodeUnavailable, "%s failed", call.Type)
		}
	}()
	sess, err := s.session(x, im, call)
	if err != nil {
		return nil, connectors.AsError(err)
	}
	if len(data) == 0 {
		data = []byte("{}")
	}
	out, err := sess.Call(x, call.Operation, data)
	if err != nil {
		return nil, connectors.AsError(err)
	}
	if len(out) == 0 {
		out = json.RawMessage(`{}`)
	}
	return out, nil
}

func key(tenant uint64, typ, connection string) string {
	return fmt.Sprintf("%d/%s/%s", tenant, typ, connection)
}

// session is the connection's open session, opened once per process.
func (s *server) session(x Context, im Impl, call connectors.ParsedCall) (Session, error) {
	k := key(call.Tenant, call.Type, call.Connection)
	s.mu.Lock()
	e, ok := s.sessions[k]
	if !ok {
		e = &entry{}
		s.sessions[k] = e
	}
	s.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sess != nil {
		return e.sess, nil
	}
	conn := Connection{ID: call.Connection, Type: call.Type, Tenant: call.Tenant}
	if call.Connection != "" && s.o.Resolve != nil {
		c, err := s.o.Resolve(x, call.Tenant, call.Type, call.Connection)
		if err != nil {
			return nil, err
		}
		conn = c
	}
	sess, err := im.Open(x, conn)
	if err != nil {
		return nil, err
	}
	e.sess = sess
	return sess, nil
}

// drop closes the sessions opened on a connection, which changed.
func (s *server) drop(tenant, connection string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.sessions {
		parts := strings.SplitN(k, "/", 3)
		if len(parts) == 3 && parts[0] == tenant && parts[2] == connection {
			delete(s.sessions, k)
			go func() {
				e.mu.Lock()
				defer e.mu.Unlock()
				if e.sess != nil {
					_ = e.sess.Close()
				}
			}()
		}
	}
}
