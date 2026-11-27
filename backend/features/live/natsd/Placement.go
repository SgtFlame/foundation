package natsd

import (
	"strconv"
	"strings"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// Placement is how many replicas a stream, KV bucket or object store keeps
// and where (forge 20a-1, documentation/Live.md › Deployed NATS). Every
// place that makes one passes its config through Stream, KV or Object, so
// one setting decides it for the whole process; TestNoRawStreamConfig
// fails on a config made without them.
type Placement struct {
	// Replicas is JetStream's replica count: 1 embedded, 3 in a deployment.
	Replicas int
	// Cluster and Tags place the stream in one cluster of a supercluster
	// (forge 20a-4); empty lets JetStream choose.
	Cluster string
	Tags    []string
}

var (
	placementMu sync.RWMutex
	placement   = Placement{Replicas: 1}
	tenants     func(tenant uint64) (Placement, bool)
)

// SetTenantPlacements sets where each tenant's streams go (forge 20a-4):
// a tenant f places has its streams, buckets and object stores made in
// that cluster (with its replicas when set), found by the tenant id in
// their names (TenantOfName). nil places none.
func SetTenantPlacements(f func(tenant uint64) (Placement, bool)) {
	placementMu.Lock()
	tenants = f
	placementMu.Unlock()
}

// TenantOfName is the tenant a stream's, bucket's or object store's name
// names: its first all-digit token between underscores (EVT_5_def,
// PRJ_5_records_record, KV_cx_secrets_5, OBJ_FILES_5). Every per-tenant
// name puts the tenant id first among its numbers.
func TenantOfName(name string) (uint64, bool) {
	for _, tok := range strings.Split(name, "_") {
		if tok == "" || strings.TrimLeft(tok, "0123456789") != "" {
			continue
		}
		if id, err := strconv.ParseUint(tok, 10, 64); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

// PlacementOf is the placement of the stream, bucket or object store named name:
// its tenant's (filled from the default where it is empty), else the
// default.
func PlacementOf(name string) Placement {
	placementMu.RLock()
	p, f := placement, tenants
	placementMu.RUnlock()
	if f == nil {
		return p
	}
	id, ok := TenantOfName(name)
	if !ok {
		return p
	}
	tp, ok := f(id)
	if !ok {
		return p
	}
	if tp.Replicas < 1 {
		tp.Replicas = p.Replicas
	}
	if tp.Cluster == "" && len(tp.Tags) == 0 {
		tp.Cluster, tp.Tags = p.Cluster, p.Tags
	}
	return tp
}

// ParsePlacements reads tenant placements written as
// "<tenant>=<cluster>[,<tenant>=<cluster>…]" (serve's --nats-placements,
// until the root tenant's desired state sets them, forge 20b-2).
func ParsePlacements(s string) (map[uint64]Placement, error) {
	out := map[uint64]Placement{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		t, cluster, ok := strings.Cut(part, "=")
		id, err := strconv.ParseUint(strings.TrimSpace(t), 10, 64)
		if !ok || err != nil || id == 0 || strings.TrimSpace(cluster) == "" {
			return nil, log.Code("bad_placement").Errorf("%q is not <tenant>=<cluster>", part)
		}
		out[id] = Placement{Cluster: strings.TrimSpace(cluster)}
	}
	return out, nil
}

// SetDefault sets the placement every later Stream, KV and Object call
// uses; serve calls it from --nats-replicas before anything is made.
func SetDefault(p Placement) {
	if p.Replicas < 1 {
		p.Replicas = 1
	}
	placementMu.Lock()
	placement = p
	placementMu.Unlock()
}

// Default is the process's placement.
func Default() Placement {
	placementMu.RLock()
	defer placementMu.RUnlock()
	return placement
}

func (p Placement) jsPlacement() *jetstream.Placement {
	if p.Cluster == "" && len(p.Tags) == 0 {
		return nil
	}
	return &jetstream.Placement{Cluster: p.Cluster, Tags: p.Tags}
}

// Stream fills a stream config's replicas and placement from the default,
// unless the config sets its own.
func Stream(c jetstream.StreamConfig) jetstream.StreamConfig {
	p := PlacementOf(c.Name)
	if c.Replicas == 0 {
		c.Replicas = p.Replicas
	}
	if c.Placement == nil {
		c.Placement = p.jsPlacement()
	}
	return c
}

// KV fills a KV bucket config's replicas and placement from the default.
func KV(c jetstream.KeyValueConfig) jetstream.KeyValueConfig {
	p := PlacementOf(c.Bucket)
	if c.Replicas == 0 {
		c.Replicas = p.Replicas
	}
	if c.Placement == nil {
		c.Placement = p.jsPlacement()
	}
	return c
}

// Object fills an object store config's replicas and placement from the
// default.
func Object(c jetstream.ObjectStoreConfig) jetstream.ObjectStoreConfig {
	p := PlacementOf(c.Bucket)
	if c.Replicas == 0 {
		c.Replicas = p.Replicas
	}
	if c.Placement == nil {
		c.Placement = p.jsPlacement()
	}
	return c
}
