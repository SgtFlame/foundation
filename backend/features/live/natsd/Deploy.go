package natsd

import (
	"context"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Environment variables for deployed NATS (forge 20a-1); serve's flags win
// over them.
const (
	EnvURL      = "FOUNDATION_NATS_URL"
	EnvCreds    = "FOUNDATION_NATS_CREDS"
	EnvCA       = "FOUNDATION_NATS_CA"
	EnvReplicas = "FOUNDATION_NATS_REPLICAS"
)

// WithEnv fills the options a flag left empty from the environment.
func (o Options) WithEnv() Options {
	if o.URL == "" {
		o.URL = os.Getenv(EnvURL)
	}
	if o.Creds == "" {
		o.Creds = os.Getenv(EnvCreds)
	}
	if o.CA == "" {
		o.CA = os.Getenv(EnvCA)
	}
	return o
}

// ReplicasOr is the flag's replica count, else $FOUNDATION_NATS_REPLICAS,
// else 1.
func ReplicasOr(flag int) int {
	if flag > 0 {
		return flag
	}
	if n, err := strconv.Atoi(os.Getenv(EnvReplicas)); err == nil && n > 0 {
		return n
	}
	return 1
}

// SetReplicas raises (or lowers) every stream's replica count, KV buckets
// and object stores included since they are streams, and returns the
// names it changed. JetStream moves the data itself; `cli nats replicas`
// calls it when a deployment grows from one node to a cluster.
func SetReplicas(ctx context.Context, js jetstream.JetStream, n int) ([]string, error) {
	var changed []string
	names := js.StreamNames(ctx)
	for name := range names.Name() {
		s, err := js.Stream(ctx, name)
		if err != nil {
			return changed, log.Wrapf(err, "read stream %s", name)
		}
		cfg := s.CachedInfo().Config
		if cfg.Replicas == n {
			continue
		}
		cfg.Replicas = n
		if _, err := js.UpdateStream(ctx, cfg); err != nil {
			return changed, log.Wrapf(err, "set the replicas of %s", name)
		}
		changed = append(changed, name)
	}
	if err := names.Err(); err != nil {
		return changed, log.Wrap(err)
	}
	return changed, nil
}

// Place moves every stream of a tenant (found by the tenant id in its
// name, TenantOfName; KV buckets and object stores included) to the
// placement p, one stream at a time, waiting until each has every replica
// current where it now is before the next (forge 20a-4, `cli nats place`).
// JetStream moves the messages; none is lost. It returns the names it
// moved; a stream already there is left alone, so a second run does
// nothing.
func Place(ctx context.Context, js jetstream.JetStream, tenant uint64, p Placement) ([]string, error) {
	var moved []string
	names := js.StreamNames(ctx)
	var all []string
	for name := range names.Name() {
		if id, ok := TenantOfName(name); ok && id == tenant {
			all = append(all, name)
		}
	}
	if err := names.Err(); err != nil {
		return nil, log.Wrap(err)
	}
	for _, name := range all {
		s, err := js.Stream(ctx, name)
		if err != nil {
			return moved, log.Wrapf(err, "read stream %s", name)
		}
		info := s.CachedInfo()
		cfg := info.Config
		if placed(info, p) {
			continue
		}
		cfg.Placement = p.jsPlacement()
		if p.Replicas > 0 {
			cfg.Replicas = p.Replicas
		}
		if _, err := js.UpdateStream(ctx, cfg); err != nil {
			return moved, log.Wrapf(err, "move stream %s", name)
		}
		if err := waitPlaced(ctx, js, name, p); err != nil {
			return moved, err
		}
		moved = append(moved, name)
	}
	return moved, nil
}

// placed tells whether a stream is where p puts it with every replica
// current: in p's cluster when it names one, else with the same placement.
func placed(info *jetstream.StreamInfo, p Placement) bool {
	if info.Cluster == nil {
		return false
	}
	if p.Cluster != "" && info.Cluster.Name != p.Cluster {
		return false
	}
	if p.Cluster == "" && !slices.Equal(placementTags(info.Config.Placement), p.Tags) {
		return false
	}
	want := info.Config.Replicas
	if p.Replicas > 0 {
		want = p.Replicas
	}
	if info.Cluster.Leader == "" || len(info.Cluster.Replicas) != want-1 {
		return false
	}
	for _, r := range info.Cluster.Replicas {
		if !r.Current || r.Offline {
			return false
		}
	}
	return true
}

func placementTags(p *jetstream.Placement) []string {
	if p == nil {
		return nil
	}
	return p.Tags
}

func waitPlaced(ctx context.Context, js jetstream.JetStream, name string, p Placement) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		s, err := js.Stream(ctx, name)
		if err == nil {
			if info, err := s.Info(ctx); err == nil && placed(info, p) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return log.Code("placement_timeout").Errorf("stream %s did not finish moving", name)
		}
		select {
		case <-ctx.Done():
			return log.Wrap(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// EnvPlacements names serve's tenant placements when --nats-placements is
// not given (forge 20a-4).
const EnvPlacements = "FOUNDATION_NATS_PLACEMENTS"

// PlacementsOr is the flag's placements, else $FOUNDATION_NATS_PLACEMENTS.
func PlacementsOr(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv(EnvPlacements)
}
