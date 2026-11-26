package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/nats-io/nats.go/jetstream"
)

// Registry is the connector registry: per tenant, the builtin contracts
// and types (the connector.contract and connector.type points), the types
// and surfaces adapters announce in cx_adapters, and the connections'
// discovered contracts. Each process keeps a cache of cx_adapters kept
// current by a KV watch: a cache of JetStream state, the same in every
// process. Scripted types (07d) are announced, per tenant, by the scripted
// adapter (connectors/scripted).
type Registry struct {
	reg   *extensions.Registry
	js    jetstream.JetStream
	every time.Duration // how often online states are checked for expiry

	once     sync.Once
	startErr error
	ready    chan struct{}
	cancel   context.CancelFunc

	mu      sync.Mutex
	ann     map[string]Announcement // valid announcements by key
	refused map[string]uint64       // keys refused, by the revision logged
	subs    map[chan struct{}]struct{}
	now     func() time.Time
}

// NewRegistry is the registry over the extension registry's builtins and
// the JetStream holding cx_adapters.
func NewRegistry(reg *extensions.Registry, js jetstream.JetStream) *Registry {
	return &Registry{reg: reg, js: js, every: time.Second, ready: make(chan struct{}), ann: map[string]Announcement{},
		refused: map[string]uint64{}, subs: map[chan struct{}]struct{}{}, now: time.Now}
}

// SetCheckEvery sets how often online states are checked (tests).
func (r *Registry) SetCheckEvery(d time.Duration) { r.every = d }

// JetStream is the JetStream the registry reads.
func (r *Registry) JetStream() jetstream.JetStream { return r.js }

// Close stops the watch.
func (r *Registry) Close() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// start watches cx_adapters once and waits for its current entries.
func (r *Registry) start(ctx context.Context) error {
	r.once.Do(func() {
		kv, err := AdaptersKV(ctx, r.js)
		if err != nil {
			r.startErr = err
			close(r.ready)
			return
		}
		wctx, cancel := context.WithCancel(context.Background())
		r.mu.Lock()
		r.cancel = cancel
		r.mu.Unlock()
		w, err := kv.WatchAll(wctx)
		if err != nil {
			cancel()
			r.startErr = err
			close(r.ready)
			return
		}
		go r.watch(wctx, w)
	})
	select {
	case <-r.ready:
		return r.startErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Registry) watch(ctx context.Context, w jetstream.KeyWatcher) {
	defer func() { _ = w.Stop() }()
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()
	readied := false
	last := r.onlineSet()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-w.Updates():
			if !ok {
				return
			}
			if e == nil {
				if !readied {
					readied = true
					close(r.ready)
				}
				continue
			}
			r.apply(ctx, e)
			if readied {
				last = r.onlineSet()
				r.notify()
			}
		case <-ticker.C:
			if now := r.onlineSet(); now != last {
				last = now
				r.notify()
			}
		}
	}
}

// apply takes one cx_adapters entry in, refusing one whose metadata fails
// the check (logged once per revision).
func (r *Registry) apply(ctx context.Context, e jetstream.KeyValueEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := e.Key()
	if e.Operation() != jetstream.KeyValuePut {
		delete(r.ann, key)
		delete(r.refused, key)
		return
	}
	var a Announcement
	if err := json.Unmarshal(e.Value(), &a); err != nil {
		r.refuse(ctx, key, e.Revision(), []Problem{{Path: "", Message: "cannot be read: " + err.Error()}})
		return
	}
	if problems := r.checkAnnouncement(a); len(problems) > 0 {
		r.refuse(ctx, key, e.Revision(), problems)
		return
	}
	delete(r.refused, key)
	r.ann[key] = a
}

func (r *Registry) refuse(ctx context.Context, key string, rev uint64, problems []Problem) {
	delete(r.ann, key)
	if r.refused[key] == rev {
		return
	}
	r.refused[key] = rev
	list := make([]string, len(problems))
	for i, p := range problems {
		list[i] = p.String()
	}
	log.Code("announcement_refused").With("key", key).Warn(ctx, "refused the connector announcement %s: %s", key, strings.Join(list, "; "))
}

// checkAnnouncement is the metadata check an announcement must pass: its
// contracts' and its type's or surface's.
func (r *Registry) checkAnnouncement(a Announcement) []Problem {
	var out []Problem
	prefix := func(pre string, ps []Problem) {
		for _, p := range ps {
			p.Path = strings.Trim(pre+"."+p.Path, ".")
			out = append(out, p)
		}
	}
	switch {
	case a.Instance == "" || strings.ContainsAny(a.Instance, ".*> "):
		out = append(out, Problem{Path: "instance", Message: "needs an instance token"})
	case a.Surface != nil:
		prefix("surface", CheckSurface(*a.Surface))
	case a.Type != nil:
		own := map[string]Contract{}
		for _, c := range a.Contracts {
			prefix("contracts."+c.Ref(), CheckContract(c))
			own[c.Ref()] = c
		}
		builtin := r.builtinContracts()
		prefix("type", CheckType(*a.Type, func(ref string) (Contract, bool) {
			if c, ok := own[ref]; ok {
				return c, true
			}
			c, ok := builtin[ref]
			return c, ok
		}))
	default:
		out = append(out, Problem{Path: "", Message: "announces neither a type nor a surface"})
	}
	return out
}

// onlineSet is a fingerprint of which announcements are online.
func (r *Registry) onlineSet() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	var keys []string
	for k, a := range r.ann {
		if a.Online(now) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return strings.Join(keys, ",")
}

func (r *Registry) notify() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Changes signals each change to what the registry lists: an
// announcement written, refused or deleted, or a type going offline.
func (r *Registry) Changes(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := r.start(ctx); err != nil {
		return nil, nil, err
	}
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}, nil
}

// builtinContracts are the connector.contract contributions by ref.
func (r *Registry) builtinContracts() map[string]Contract {
	out := map[string]Contract{}
	if r.reg == nil {
		return out
	}
	for _, c := range Contracts.All(r.reg) {
		out[c.Contract.Ref()] = c.Contract
	}
	return out
}

// ContractInfo is a contract as the registry lists it.
type ContractInfo struct {
	Contract Contract `json:"contract"`
	Ref      string   `json:"ref"`
	Source   string   `json:"source"` // builtin, announced or discovered
	Types    []string `json:"types"`  // the types implementing it
	Online   bool     `json:"online"` // one of its types is online
	// NeedsConnection is set when a call names a connection: some type
	// implementing it needs one.
	NeedsConnection bool `json:"needsConnection"`
}

// TypeInfo is a type as the registry lists it.
type TypeInfo struct {
	Type      Type   `json:"type"`
	Source    string `json:"source"` // builtin, scripted or announced
	Direction string `json:"direction"`
	Online    bool   `json:"online"`
	Instances int    `json:"instances"`
}

// SurfaceEntry is a surface as the registry lists it.
type SurfaceEntry struct {
	Surface   SurfaceInfo `json:"surface"`
	Direction string      `json:"direction"`
	Online    bool        `json:"online"`
	Instances int         `json:"instances"`
}

// Catalog is everything the registry lists for a tenant.
type Catalog struct {
	Contracts []ContractInfo `json:"contracts"`
	Types     []TypeInfo     `json:"types"`
	Surfaces  []SurfaceEntry `json:"surfaces"`
}

// Sources a listed contract or type comes from.
const (
	FromBuiltin    = "builtin"
	FromAnnounced  = "announced"
	FromDiscovered = "discovered"
)

// Catalog lists a tenant's contracts, types and surfaces, each with
// whether it is online.
func (r *Registry) Catalog(ctx context.Context, tenant uint64) (Catalog, error) {
	if err := r.start(ctx); err != nil {
		return Catalog{}, err
	}
	contracts := map[string]*ContractInfo{}
	types := map[string]*TypeInfo{}
	surfaces := map[string]*SurfaceEntry{}
	if r.reg != nil {
		for _, c := range Contracts.All(r.reg) {
			contracts[c.Contract.Ref()] = &ContractInfo{Contract: c.Contract, Ref: c.Contract.Ref(), Source: FromBuiltin, Types: []string{}}
		}
		for _, t := range Types.All(r.reg) {
			src := FromBuiltin
			if t.Type.Source == SourceScripted {
				src = SourceScripted
			}
			types[t.Type.ID] = &TypeInfo{Type: t.Type, Source: src, Direction: Outbound}
		}
	}
	r.mu.Lock()
	now := r.now()
	keys := make([]string, 0, len(r.ann))
	for k := range r.ann {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		a := r.ann[k]
		if !a.Serves(tenant) {
			continue
		}
		online := a.Online(now)
		switch {
		case a.Surface != nil:
			s := surfaces[a.Surface.ID]
			if s == nil {
				s = &SurfaceEntry{Surface: *a.Surface, Direction: Inbound}
				surfaces[a.Surface.ID] = s
			}
			if online {
				s.Online, s.Instances = true, s.Instances+1
			}
		case a.Type != nil:
			for _, c := range a.Contracts {
				if _, ok := contracts[c.Ref()]; !ok {
					contracts[c.Ref()] = &ContractInfo{Contract: c, Ref: c.Ref(), Source: FromAnnounced, Types: []string{}}
				}
			}
			t := types[a.Type.ID]
			if t == nil {
				src := FromAnnounced
				if a.Type.Source == SourceScripted {
					src = SourceScripted // an app's, served by the scripted adapter (07d)
				}
				t = &TypeInfo{Type: *a.Type, Source: src, Direction: Outbound}
				types[a.Type.ID] = t
			}
			if online {
				t.Online, t.Instances = true, t.Instances+1
			}
		}
	}
	r.mu.Unlock()
	for _, t := range types {
		for _, ref := range t.Type.Contracts {
			if c, ok := contracts[ref]; ok {
				c.Types = append(c.Types, t.Type.ID)
				c.Online = c.Online || t.Online
				c.NeedsConnection = c.NeedsConnection || !t.Type.NoConnection
			}
		}
	}
	out := Catalog{Contracts: []ContractInfo{}, Types: []TypeInfo{}, Surfaces: []SurfaceEntry{}}
	for _, c := range contracts {
		slices.Sort(c.Types)
		out.Contracts = append(out.Contracts, *c)
	}
	for _, t := range types {
		out.Types = append(out.Types, *t)
	}
	for _, s := range surfaces {
		out.Surfaces = append(out.Surfaces, *s)
	}
	slices.SortFunc(out.Contracts, func(a, b ContractInfo) int { return strings.Compare(a.Ref, b.Ref) })
	slices.SortFunc(out.Types, func(a, b TypeInfo) int { return strings.Compare(a.Type.ID, b.Type.ID) })
	slices.SortFunc(out.Surfaces, func(a, b SurfaceEntry) int { return strings.Compare(a.Surface.ID, b.Surface.ID) })
	return out, nil
}

// Contract finds a contract by reference for a tenant: id@version, an id
// alone (its latest version), or conn.<connection>, a connection's
// discovered contract.
func (r *Registry) Contract(ctx context.Context, tenant uint64, ref string) (ContractInfo, bool, error) {
	if conn, ok := strings.CutPrefix(ref, DiscoveredPrefix); ok {
		conn, _, _ = strings.Cut(conn, "@")
		d, ok, err := GetDiscovered(ctx, r.js, tenant, conn)
		if err != nil || !ok {
			return ContractInfo{}, false, err
		}
		return ContractInfo{Contract: d.Contract, Ref: d.Contract.Ref(), Source: FromDiscovered, Types: []string{}, Online: true, NeedsConnection: true}, true, nil
	}
	id, version, err := ParseRef(ref)
	if err != nil {
		return ContractInfo{}, false, err
	}
	cat, err := r.Catalog(ctx, tenant)
	if err != nil {
		return ContractInfo{}, false, err
	}
	var best *ContractInfo
	for i, c := range cat.Contracts {
		if c.Contract.ID != id || (version != 0 && c.Contract.Version != version) {
			continue
		}
		if best == nil || c.Contract.Version > best.Contract.Version {
			best = &cat.Contracts[i]
		}
	}
	if best == nil {
		return ContractInfo{}, false, nil
	}
	return *best, true, nil
}

// Type finds a type for a tenant.
func (r *Registry) Type(ctx context.Context, tenant uint64, id string) (TypeInfo, bool, error) {
	cat, err := r.Catalog(ctx, tenant)
	if err != nil {
		return TypeInfo{}, false, err
	}
	for _, t := range cat.Types {
		if t.Type.ID == id {
			return t, true, nil
		}
	}
	return TypeInfo{}, false, nil
}

// Describe is connectors.describe's answer for a ref: contract:<id[@v]>
// (with the types implementing it), connector:<type> (with its
// contracts), surface:<id> or connection:<id> (a discovered contract with
// its problems).
func (r *Registry) Describe(ctx context.Context, tenant uint64, ref string) (map[string]any, error) {
	kind, id, _ := strings.Cut(ref, ":")
	switch kind {
	case "contract":
		c, ok, err := r.Contract(ctx, tenant, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, log.Code("contract_not_found").Errorf("there is no contract %s; list them with connectors.contracts", id)
		}
		cat, _ := r.Catalog(ctx, tenant)
		var impl []TypeInfo
		for _, t := range cat.Types {
			if slices.Contains(c.Types, t.Type.ID) {
				impl = append(impl, t)
			}
		}
		return map[string]any{"ref": ref, "kind": "contract", "contract": c.Contract, "source": c.Source, "online": c.Online,
			"needsConnection": c.NeedsConnection, "types": impl}, nil
	case "connector":
		t, ok, err := r.Type(ctx, tenant, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, log.Code("connector_not_found").Errorf("there is no connector type %s; list them with connectors.types", id)
		}
		var contracts []Contract
		for _, cref := range t.Type.Contracts {
			if c, ok, _ := r.Contract(ctx, tenant, cref); ok {
				contracts = append(contracts, c.Contract)
			}
		}
		return map[string]any{"ref": ref, "kind": "connector", "type": t.Type, "source": t.Source, "direction": t.Direction,
			"online": t.Online, "instances": t.Instances, "contracts": contracts}, nil
	case "surface":
		cat, err := r.Catalog(ctx, tenant)
		if err != nil {
			return nil, err
		}
		for _, s := range cat.Surfaces {
			if s.Surface.ID == id {
				return map[string]any{"ref": ref, "kind": "surface", "surface": s.Surface, "direction": s.Direction, "online": s.Online, "instances": s.Instances}, nil
			}
		}
		return nil, log.Code("surface_not_found").Errorf("there is no surface %s", id)
	case "connection":
		d, ok, err := GetDiscovered(ctx, r.js, tenant, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, log.Code("contract_not_found").Errorf("connection %s has no discovered contract", id)
		}
		problems := d.Problems
		if problems == nil {
			problems = []Problem{}
		}
		return map[string]any{"ref": ref, "kind": "connection", "contract": d.Contract, "source": FromDiscovered, "problems": problems, "at": d.At}, nil
	}
	return nil, log.Code("bad_ref").Errorf("%q is not a connector ref: use contract:<id>, connector:<type>, surface:<id> or connection:<id>", ref)
}

// String names a ref for messages.
func (c ContractInfo) String() string { return fmt.Sprintf("%s (%s)", c.Ref, c.Source) }
