// Package hooks is the hook endpoint (forge 26b-1, documentation/
// Connectors.md › Built in forge 26b-1): POST /hooks/cx/<hookId> takes a
// callback a service pushes for one connection, has the connection's type
// verify it over the connector bus (cx.receive, answered by its adapter
// with Receive, which publishes the events to CX_<tenant> under their ids
// before replying), and answers 2xx only once they are kept. Hooks are
// made per connection (Ensure) in KV cx_hooks, shared by every process, so
// any process serves any hook; the rate limit per hook is a token bucket
// in KV cx_hook_rates, shared the same way.
package hooks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("connectors").Tags("hooks")

// Bucket keeps the hooks: hook.<id> → Hook, and conn.<tenant>.<connection>
// → the connection's hook id.
const Bucket = "cx_hooks"

// RatesBucket keeps each hook's token bucket.
const RatesBucket = "cx_hook_rates"

// Path is where hooks are served; a hook's URL is Path + its id.
const Path = "/hooks/cx/"

// Hook is one connection's endpoint.
type Hook struct {
	ID         string    `json:"id"`
	Tenant     uint64    `json:"tenant,string"`
	Connection string    `json:"connection"`
	Type       string    `json:"type"`
	CreatedAt  time.Time `json:"createdAt"`
}

// URL is the hook's URL under a public base (https://axiom.example.com).
func (h Hook) URL(base string) string { return base + Path + h.ID }

func kvOf(ctx context.Context, js jetstream.JetStream, bucket, about string, ttl time.Duration) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, bucket)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		kv, err = js.CreateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: bucket, History: 1, TTL: ttl, Description: about}))
		if errors.Is(err, jetstream.ErrBucketExists) {
			kv, err = js.KeyValue(ctx, bucket)
		}
	}
	return kv, err
}

// KV is the cx_hooks bucket, made on first use.
func KV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	return kvOf(ctx, js, Bucket, "Connector hooks: hook.<id> and conn.<tenant>.<connection> (forge 26b-1)", 0)
}

func connKey(tenant uint64, connection string) string {
	return fmt.Sprintf("conn.%d.%s", tenant, connection)
}

// NewID is a hook id: 128 random bits, as hex.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ValidID reports whether id could be a hook id.
func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// Ensure gives the connection's hook, making it the first time. Two
// processes making one at once agree on one: the connection's key is
// created once, and the loser's hook is removed.
func Ensure(ctx context.Context, js jetstream.JetStream, tenant uint64, connection, typ string) (Hook, error) {
	kv, err := KV(ctx, js)
	if err != nil {
		return Hook{}, log.Wrap(err)
	}
	if h, ok, err := ofConnection(ctx, kv, tenant, connection); err != nil || ok {
		return h, err
	}
	h := Hook{ID: NewID(), Tenant: tenant, Connection: connection, Type: typ, CreatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(h)
	if _, err := kv.Create(ctx, "hook."+h.ID, raw); err != nil {
		return Hook{}, log.Wrap(err)
	}
	if _, err := kv.Create(ctx, connKey(tenant, connection), []byte(h.ID)); err != nil {
		_ = kv.Delete(ctx, "hook."+h.ID)
		if errors.Is(err, jetstream.ErrKeyExists) {
			if other, ok, err := ofConnection(ctx, kv, tenant, connection); err != nil || ok {
				return other, err
			}
		}
		return Hook{}, log.Wrap(err)
	}
	return h, nil
}

func ofConnection(ctx context.Context, kv jetstream.KeyValue, tenant uint64, connection string) (Hook, bool, error) {
	e, err := kv.Get(ctx, connKey(tenant, connection))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return Hook{}, false, nil
	}
	if err != nil {
		return Hook{}, false, log.Wrap(err)
	}
	return Lookup(ctx, kv, string(e.Value()))
}

// Of reads a connection's hook, if it has one.
func Of(ctx context.Context, js jetstream.JetStream, tenant uint64, connection string) (Hook, bool, error) {
	kv, err := KV(ctx, js)
	if err != nil {
		return Hook{}, false, log.Wrap(err)
	}
	return ofConnection(ctx, kv, tenant, connection)
}

// Lookup reads a hook by its id.
func Lookup(ctx context.Context, kv jetstream.KeyValue, id string) (Hook, bool, error) {
	if !ValidID(id) {
		return Hook{}, false, nil
	}
	e, err := kv.Get(ctx, "hook."+id)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return Hook{}, false, nil
	}
	if err != nil {
		return Hook{}, false, log.Wrap(err)
	}
	var h Hook
	if err := json.Unmarshal(e.Value(), &h); err != nil {
		return Hook{}, false, log.Wrap(err)
	}
	return h, true, nil
}

// Drop removes a connection's hook: its URL answers 404 from then on.
func Drop(ctx context.Context, js jetstream.JetStream, tenant uint64, connection string) error {
	kv, err := KV(ctx, js)
	if err != nil {
		return log.Wrap(err)
	}
	h, ok, err := ofConnection(ctx, kv, tenant, connection)
	if err != nil || !ok {
		return err
	}
	if err := kv.Delete(ctx, "hook."+h.ID); err != nil {
		return log.Wrap(err)
	}
	return log.Wrap(kv.Delete(ctx, connKey(tenant, connection)))
}
