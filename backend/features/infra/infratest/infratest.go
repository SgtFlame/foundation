// Package infratest is the Infrastructure test server (forge 22c): a
// builtin connector type, infratest, that implements infra.compute@1 over
// hosts it keeps in memory, with failure injection through its own
// contract infratest.control@1, so every infrastructure part is tested
// without a cloud account.
//
// A created host is creating until the connection's delayMs passes, then
// running; host.changed events report each change to a Watch. Its state
// lives in the process that serves the type (a test server, not a
// provider): give it one adapter instance.
package infratest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// TypeID is the test server's connector type; it is also the provider
// name a configuration's account gives (infra.Providers).
const (
	TypeID     = "infratest"
	ControlID  = "infratest.control"
	ControlRef = "infratest.control@1"
)

const settings = `{"type":"object","properties":{
  "delayMs":{"type":"integer","minimum":0,"title":"Delay","description":"Milliseconds a created host stays creating before it is running"}}}`

// Type is the test server's connector type.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{infra.ComputeRef, ControlRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings),
		Meta: connectors.Meta{Title: "Infrastructure test server", Description: "Makes pretend hosts in memory: implements infra.compute@1 with sizes s, m, l and xl (l and xl run VMs) in regions r1 and r2, and lets tests fail, slow or vanish hosts through infratest.control@1. For tests and trying infrastructure by hand; it makes nothing real.", Tags: []string{"infrastructure", "test"}},
	}
}

// ControlContract is infratest.control@1: failure injection.
func ControlContract() connectors.Contract {
	hostsResult := json.RawMessage(`{"type":"object","properties":{"hosts":{"type":"array","description":"Every host, gone ones too","items":{"type":"object","properties":{
  "id":{"type":"string","description":"The host's resource id"},
  "state":{"type":"string","description":"Its state"},
  "size":{"type":"string","description":"Its size"},
  "region":{"type":"string","description":"Its region"}},"required":["id","state"]}},
  "faults":{"type":"array","description":"Faults still to happen","items":{"type":"object","properties":{
  "operation":{"type":"string","description":"The operation"},
  "times":{"type":"integer","description":"Calls left to fail"},
  "kind":{"type":"string","description":"The error code"},
  "slowMs":{"type":"integer","description":"Milliseconds each call waits"}},"required":["operation"]}}},"required":["hosts","faults"]}`)
	ok := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true"}},"required":["ok"]}`)
	return connectors.Contract{
		ID: ControlID, Version: 1,
		Meta: connectors.Meta{Title: "Infrastructure test server control", Description: "Makes the Infrastructure test server misbehave: fail or slow its next calls, or delete a host behind the configuration's back, and shows its hosts and pending faults. For tests only.", Tags: []string{"infrastructure", "test"}},
		Operations: []connectors.Operation{
			{
				ID: "fail", Title: "Fail the next calls", Description: "Makes the next times calls of an infra.compute operation fail with the error kind given (rateLimited and unavailable are retried by jobs).",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "operation":{"type":"string","description":"The infra.compute operation, e.g. host.create"},
  "times":{"type":"integer","minimum":1,"description":"How many calls fail; once when left out"},
  "kind":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code; unavailable when left out"}},"required":["operation"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"host.create","times":3,"kind":"unavailable"}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "slow", Title: "Slow the calls", Description: "Makes every call of an infra.compute operation wait ms milliseconds first; 0 stops it.",
				Effect: connectors.EffectWrite,
				Args: json.RawMessage(`{"type":"object","properties":{
  "operation":{"type":"string","description":"The infra.compute operation"},
  "ms":{"type":"integer","minimum":0,"description":"Milliseconds each call waits"}},"required":["operation","ms"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"operation":"host.get","ms":200}`), Result: json.RawMessage(`{"ok":true}`)}},
			},
			{
				ID: "vanish", Title: "Vanish a host", Description: "Deletes a host behind the configuration's back, as if someone removed it at the provider, for drift tests.",
				Effect: connectors.EffectWrite, Destructive: true,
				Args:   json.RawMessage(`{"type":"object","properties":{"host":{"type":"string","description":"The host's resource id"}},"required":["host"]}`),
				Result: ok, Examples: []connectors.Example{{Args: json.RawMessage(`{"host":"west-general-1"}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors: []string{connectors.CodeNotFound},
			},
			{
				ID: "state", Title: "Show the state", Description: "Every host the connection has, gone ones too, and the faults still to happen.",
				Effect: connectors.EffectRead,
				Args:   json.RawMessage(`{"type":"object","properties":{}}`),
				Result: hostsResult, Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"hosts":[{"id":"west-general-1","state":"running","size":"m","region":"r1"}],"faults":[]}`)}},
			},
		},
	}
}

// Contribute registers the test server's type and control contract.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, TypeID, []connectors.Contract{ControlContract()}, []connectors.Type{Type()})
}

// Sizes are the test server's sizes.
var Sizes = []infra.ProviderSize{
	{ID: "s", VCPUs: 1, MemoryMB: 1024, DiskGB: 25, Price: 0.01},
	{ID: "m", VCPUs: 2, MemoryMB: 4096, DiskGB: 80, Price: 0.04},
	{ID: "l", VCPUs: 4, MemoryMB: 16384, DiskGB: 160, Price: 0.16, VMs: true},
	{ID: "xl", VCPUs: 8, MemoryMB: 32768, DiskGB: 320, Price: 0.32, VMs: true},
}

// Regions are the test server's regions.
var Regions = []map[string]string{{"id": "r1", "name": "Region one"}, {"id": "r2", "name": "Region two"}}

// Images are the test server's images.
var Images = []map[string]string{{"id": "ubuntu-24.04", "name": "Ubuntu 24.04 LTS"}, {"id": "ubuntu-22.04", "name": "Ubuntu 22.04 LTS"}}

// Server holds every connection's hosts in this process.
type Server struct {
	mu    sync.Mutex
	conns map[string]*account
	now   func() time.Time
}

// New makes an empty server.
func New() *Server { return &Server{conns: map[string]*account{}, now: time.Now} }

// Default is this process's server.
var Default = New()

type host struct {
	infra.ProviderHost
	created time.Time
	ready   bool // creating became running (reported)
}

type fault struct {
	times  int
	kind   string
	slowMs int
}

type event struct {
	seq   int
	host  string
	state string
	at    time.Time
}

type account struct {
	delay  time.Duration
	hosts  map[string]*host
	faults map[string]*fault
	events []event
	next   int
	wake   chan struct{}
}

func (s *Server) account(tenant uint64, conn string) *account {
	k := strconv.FormatUint(tenant, 10) + "." + conn
	a := s.conns[k]
	if a == nil {
		a = &account{hosts: map[string]*host{}, faults: map[string]*fault{}, wake: make(chan struct{})}
		s.conns[k] = a
	}
	return a
}

// emit logs a host's change and wakes the watches; under s.mu.
func (a *account) emit(h *host, at time.Time) {
	a.events = append(a.events, event{seq: len(a.events) + 1, host: h.ID, state: h.State, at: at})
	close(a.wake)
	a.wake = make(chan struct{})
}

// tick turns hosts whose delay passed running; under s.mu.
func (s *Server) tick(a *account) {
	now := s.now()
	ids := make([]string, 0, len(a.hosts))
	for id := range a.hosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		h := a.hosts[id]
		if h.State == infra.HostCreating && !now.Before(h.created.Add(a.delay)) {
			h.State = infra.HostRunning
			h.ready = true
			a.emit(h, now)
		}
	}
}

// Impl is the adapter implementation of the test server's type.
func (s *Server) Impl() adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st struct {
			DelayMs int `json:"delayMs"`
		}
		_ = json.Unmarshal(conn.Settings, &st)
		s.mu.Lock()
		s.account(ctx.Tenant(), conn.ID).delay = time.Duration(st.DelayMs) * time.Millisecond
		s.mu.Unlock()
		return &session{s: s, tenant: ctx.Tenant(), conn: conn.ID}, nil
	}}
}

// Options are adapter options serving only the test server, for tests.
func (s *Server) Options(name string) adapter.Options {
	return adapter.Options{Name: name, Types: []connectors.Type{Type()}, Contracts: []connectors.Contract{infra.ComputeContract(), ControlContract()}, Impls: []adapter.Impl{s.Impl()}}
}

type session struct {
	s      *Server
	tenant uint64
	conn   string
}

func (x *session) Close() error { return nil }

func (x *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	for {
		x.s.mu.Lock()
		a := x.s.account(x.tenant, x.conn)
		x.s.tick(a)
		pending := append([]event(nil), a.events[min(from, len(a.events)):]...)
		wake := a.wake
		x.s.mu.Unlock()
		for _, e := range pending {
			id := fmt.Sprintf("%s-%d", x.conn, e.seq)
			payload, _ := json.Marshal(map[string]any{"id": id, "host": e.host, "state": e.state, "at": e.at.UTC().Format(time.RFC3339)})
			c, _ := json.Marshal(e.seq)
			if err := emit(adapter.Event{Contract: infra.ComputeRef, Name: "host.changed", ID: id, Payload: payload, Cursor: c}); err != nil {
				return err
			}
			from = e.seq
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	if ctx.Contract == ControlRef {
		return x.control(op, args)
	}
	x.s.mu.Lock()
	a := x.s.account(x.tenant, x.conn)
	f := a.faults[op]
	var slow int
	var fail *connectors.Error
	if f != nil {
		slow = f.slowMs
		if f.times > 0 {
			f.times--
			fail = connectors.Errorf(f.kind, "the test server failed %s as asked", op)
		}
		if f.times == 0 && f.slowMs == 0 {
			delete(a.faults, op)
		}
	}
	x.s.mu.Unlock()
	if slow > 0 {
		select {
		case <-ctx.Done():
			return nil, connectors.Errorf(connectors.CodeUnavailable, "cancelled")
		case <-time.After(time.Duration(slow) * time.Millisecond):
		}
	}
	if fail != nil {
		return nil, fail
	}
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	a = x.s.account(x.tenant, x.conn)
	x.s.tick(a)
	var in struct {
		ID, Name, Size, Image, Region, UserData string
		Nested                                  bool
	}
	_ = json.Unmarshal(args, &in)
	now := x.s.now()
	switch op {
	case "check":
		return json.RawMessage(`{"ok":true}`), nil
	case "size.list":
		return json.Marshal(map[string]any{"sizes": Sizes})
	case "image.list":
		return json.Marshal(map[string]any{"images": Images})
	case "region.list":
		return json.Marshal(map[string]any{"regions": Regions})
	case "host.create":
		if h := a.hosts[in.ID]; h != nil && h.State != infra.HostGone {
			return json.Marshal(h.ProviderHost)
		}
		size, ok := sizeOf(in.Size)
		if !ok {
			return nil, connectors.Errorf(connectors.CodeInvalid, "there is no size %q", in.Size)
		}
		if !regionOK(in.Region) {
			return nil, connectors.Errorf(connectors.CodeInvalid, "there is no region %q", in.Region)
		}
		if in.Nested && !size.VMs {
			return nil, connectors.Errorf(connectors.CodeInvalid, "size %s cannot run virtual machines", in.Size)
		}
		if in.Image == "" {
			in.Image = "ubuntu-24.04"
		}
		if in.Name == "" {
			in.Name = in.ID
		}
		a.next++
		h := &host{created: now, ProviderHost: infra.ProviderHost{
			ID: in.ID, ProviderID: fmt.Sprintf("vm-%04d", a.next), Name: in.Name, Size: in.Size, Region: in.Region, Image: in.Image,
			State: infra.HostCreating, Nested: in.Nested, Addresses: []string{fmt.Sprintf("10.0.%d.%d", a.next/250, a.next%250+4)},
			Facts: map[string]any{"zone": in.Region + "-a", "userData": in.UserData != ""},
		}}
		a.hosts[in.ID] = h
		a.emit(h, now)
		x.s.tick(a)
		return json.Marshal(h.ProviderHost)
	case "host.get", "host.start", "host.stop":
		h := a.hosts[in.ID]
		if h == nil || h.State == infra.HostGone {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no host %q", in.ID)
		}
		switch {
		case op == "host.start" && h.State == infra.HostStopped:
			h.State = infra.HostRunning
			a.emit(h, now)
		case op == "host.stop" && h.State == infra.HostRunning:
			h.State = infra.HostStopped
			a.emit(h, now)
		case op != "host.get" && h.State == infra.HostCreating:
			return nil, connectors.Errorf(connectors.CodeConflict, "host %q is still creating", in.ID)
		}
		return json.Marshal(h.ProviderHost)
	case "host.list":
		var region struct{ Region string }
		_ = json.Unmarshal(args, &region)
		hosts := []infra.ProviderHost{}
		for _, h := range a.sorted() {
			if h.State != infra.HostGone && (region.Region == "" || h.Region == region.Region) {
				hosts = append(hosts, h.ProviderHost)
			}
		}
		return json.Marshal(map[string]any{"hosts": hosts})
	case "host.delete":
		h := a.hosts[in.ID]
		if h == nil || h.State == infra.HostGone {
			return json.RawMessage(`{"deleted":false}`), nil
		}
		h.State = infra.HostGone
		h.Addresses = []string{}
		a.emit(h, now)
		return json.RawMessage(`{"deleted":true}`), nil
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
}

func (a *account) sorted() []*host {
	out := make([]*host, 0, len(a.hosts))
	for _, h := range a.hosts {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (x *session) control(op string, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Operation, Kind, Host string
		Times, Ms             int
	}
	_ = json.Unmarshal(args, &in)
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	a := x.s.account(x.tenant, x.conn)
	f := a.faults[in.Operation]
	if f == nil && (op == "fail" || op == "slow") {
		if _, ok := infra.ComputeContract().Operation(in.Operation); !ok {
			return nil, connectors.Errorf(connectors.CodeInvalid, "infra.compute has no operation %q", in.Operation)
		}
		f = &fault{}
		a.faults[in.Operation] = f
	}
	switch op {
	case "fail":
		f.times = max(in.Times, 1)
		f.kind = in.Kind
		if f.kind == "" {
			f.kind = connectors.CodeUnavailable
		}
	case "slow":
		f.slowMs = in.Ms
		if f.slowMs == 0 && f.times == 0 {
			delete(a.faults, in.Operation)
		}
	case "vanish":
		h := a.hosts[in.Host]
		if h == nil || h.State == infra.HostGone {
			return nil, connectors.Errorf(connectors.CodeNotFound, "there is no host %q", in.Host)
		}
		h.State = infra.HostGone
		a.emit(h, x.s.now())
	case "state":
		x.s.tick(a)
		hosts := []map[string]any{}
		for _, h := range a.sorted() {
			hosts = append(hosts, map[string]any{"id": h.ID, "state": h.State, "size": h.Size, "region": h.Region})
		}
		faults := []map[string]any{}
		ops := make([]string, 0, len(a.faults))
		for o := range a.faults {
			ops = append(ops, o)
		}
		sort.Strings(ops)
		for _, o := range ops {
			f := a.faults[o]
			faults = append(faults, map[string]any{"operation": o, "times": f.times, "kind": f.kind, "slowMs": f.slowMs})
		}
		return json.Marshal(map[string]any{"hosts": hosts, "faults": faults})
	default:
		return nil, connectors.Errorf(connectors.CodeNotSupported, "no operation %q", op)
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func sizeOf(id string) (infra.ProviderSize, bool) {
	for _, s := range Sizes {
		if s.ID == id {
			return s, true
		}
	}
	return infra.ProviderSize{}, false
}

func regionOK(id string) bool {
	for _, r := range Regions {
		if r["id"] == id {
			return true
		}
	}
	return false
}
