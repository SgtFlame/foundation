package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// SessionAge is how long a session's record lasts without being stored
// again; an open session stores it every SessionRefresh.
const (
	SessionAge     = 10 * time.Minute
	SessionRefresh = 3 * time.Minute
)

// KVSessions keeps session records in the tenant's ui_sessions_<tenant>
// bucket (01-ServerUI question 2), so a restart or another instance
// resumes a session's view state.
type KVSessions struct {
	js      jetstream.JetStream
	mu      sync.Mutex
	buckets map[uint64]jetstream.KeyValue
}

func NewKVSessions(js jetstream.JetStream) *KVSessions {
	return &KVSessions{js: js, buckets: map[uint64]jetstream.KeyValue{}}
}

func (k *KVSessions) bucket(ctx context.Context, tenant uint64) (jetstream.KeyValue, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if kv := k.buckets[tenant]; kv != nil {
		return kv, nil
	}
	kv, err := k.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: fmt.Sprintf("ui_sessions_%d", tenant), Description: "UI sessions' view state", History: 1, TTL: SessionAge, Storage: jetstream.FileStorage,
	})
	if err != nil {
		return nil, Log.Wrapf(err, "cannot make the UI sessions bucket")
	}
	k.buckets[tenant] = kv
	return kv, nil
}

func (k *KVSessions) Load(ctx context.Context, tenant uint64, id string) (*Record, error) {
	kv, err := k.bucket(ctx, tenant)
	if err != nil {
		return nil, err
	}
	entry, err := kv.Get(ctx, id)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, Log.Wrap(err)
	}
	var r Record
	if err := json.Unmarshal(entry.Value(), &r); err != nil {
		return nil, nil // unreadable: open afresh
	}
	return &r, nil
}

func (k *KVSessions) Save(ctx context.Context, r Record) error {
	kv, err := k.bucket(ctx, r.TenantID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Log.Wrap(err)
	}
	_, err = kv.Put(ctx, r.ID, raw)
	return Log.Wrap(err)
}

func (k *KVSessions) Delete(ctx context.Context, tenant uint64, id string) error {
	kv, err := k.bucket(ctx, tenant)
	if err != nil {
		return err
	}
	if err := kv.Delete(ctx, id); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return Log.Wrap(err)
	}
	return nil
}
