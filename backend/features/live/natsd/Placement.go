package natsd

import (
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
)

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
	p := Default()
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
	p := Default()
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
	p := Default()
	if c.Replicas == 0 {
		c.Replicas = p.Replicas
	}
	if c.Placement == nil {
		c.Placement = p.jsPlacement()
	}
	return c
}
