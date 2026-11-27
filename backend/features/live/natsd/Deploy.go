package natsd

import (
	"context"
	"os"
	"strconv"

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
