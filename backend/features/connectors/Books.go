package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Books is the bookkeeping an adapter runtime keeps (forge 21e-1): its
// announcements, discovered contracts, the call ledger, exclusive locks,
// event sources with their leases and cursors, and the events it
// publishes. InJetStream keeps them in the cx_* buckets and CX_<tenant>
// streams; connectorsservice.Remote reaches them through svc.connectors,
// so an adapter needs no JetStream handle.
type Books interface {
	Announce(ctx context.Context, a Announcement) error
	// Withdraw removes an announcement at once.
	Withdraw(ctx context.Context, key string) error
	// Announcements are the current ones, online or not.
	Announcements(ctx context.Context) ([]Announcement, error)
	PutDiscovered(ctx context.Context, tenant uint64, connection string, c Contract) (Discovered, error)
	LedgerPut(ctx context.Context, tenant uint64, connection, op, key string, e LedgerEntry) error
	// Lock takes a connection's exclusive lock, trying until ctx ends.
	Lock(ctx context.Context, tenant uint64, connection, holder string) (release func(), err error)
	// Sources sends the event sources, then their changes, until ctx
	// ends: Ready once the current ones are in.
	Sources(ctx context.Context) (<-chan SourceUpdate, error)
	// LeaseCreate takes a free source's lease; LeaseUpdate refreshes one
	// at the revision it holds, failing with ErrLeaseLost when it moved
	// on or ended; LeaseDelete gives it back.
	LeaseCreate(ctx context.Context, key string, l Lease) (uint64, error)
	LeaseUpdate(ctx context.Context, key string, l Lease, rev uint64) (uint64, error)
	LeaseDelete(ctx context.Context, key string, rev uint64) error
	Cursor(ctx context.Context, key string) (Cursor, bool, error)
	PutCursor(ctx context.Context, key string, c Cursor) error
	// Publish puts one event in CX_<tenant> (PublishEvent).
	Publish(ctx context.Context, tenant uint64, connection, contract, event, id string, payload json.RawMessage, at time.Time) error
}

// SourceUpdate is one change to the event sources: a source put (Source
// set) or gone (nil), or Ready once the current ones were sent.
type SourceUpdate struct {
	Key    string  `json:"key,omitempty"`
	Source *Source `json:"source,omitempty"`
	Ready  bool    `json:"ready,omitempty"`
}

// ErrLeaseLost is a lease refresh refused because the lease moved on or
// ended.
var ErrLeaseLost = errors.New("the lease was lost")

// ErrLeaseTaken is a lease another instance holds.
var ErrLeaseTaken = errors.New("the lease is held")

// BooksOn keeps the books in nc's JetStream.
func BooksOn(nc *nats.Conn) (Books, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return InJetStream(js), nil
}

// InJetStream keeps the books in js.
func InJetStream(js jetstream.JetStream) Books { return jsBooks{js} }

type jsBooks struct{ js jetstream.JetStream }

func (b jsBooks) Announce(ctx context.Context, a Announcement) error {
	kv, err := AdaptersKV(ctx, b.js)
	if err != nil {
		return err
	}
	return Announce(ctx, kv, a)
}

func (b jsBooks) Withdraw(ctx context.Context, key string) error {
	kv, err := AdaptersKV(ctx, b.js)
	if err != nil {
		return err
	}
	if err := kv.Delete(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return err
	}
	return nil
}

func (b jsBooks) Announcements(ctx context.Context) ([]Announcement, error) {
	kv, err := AdaptersKV(ctx, b.js)
	if err != nil {
		return nil, err
	}
	keys, err := kv.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	var out []Announcement
	for k := range keys.Keys() {
		e, err := kv.Get(ctx, k)
		if err != nil {
			continue
		}
		var a Announcement
		if json.Unmarshal(e.Value(), &a) == nil {
			out = append(out, a)
		}
	}
	return out, nil
}

func (b jsBooks) PutDiscovered(ctx context.Context, tenant uint64, connection string, c Contract) (Discovered, error) {
	return PutDiscovered(ctx, b.js, tenant, connection, c)
}

func (b jsBooks) LedgerPut(ctx context.Context, tenant uint64, connection, op, key string, e LedgerEntry) error {
	return LedgerPut(ctx, b.js, tenant, connection, op, key, e)
}

func (b jsBooks) Lock(ctx context.Context, tenant uint64, connection, holder string) (func(), error) {
	return Lock(ctx, b.js, tenant, connection, holder)
}

func (b jsBooks) Sources(ctx context.Context) (<-chan SourceUpdate, error) {
	kv, err := SourcesKV(ctx, b.js)
	if err != nil {
		return nil, err
	}
	w, err := kv.WatchAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan SourceUpdate)
	go func() {
		defer close(out)
		defer func() { _ = w.Stop() }()
		for {
			var u SourceUpdate
			select {
			case <-ctx.Done():
				return
			case e, ok := <-w.Updates():
				if !ok {
					return
				}
				switch {
				case e == nil:
					u.Ready = true
				case e.Operation() != jetstream.KeyValuePut:
					u.Key = e.Key()
				default:
					u.Key = e.Key()
					var src Source
					if json.Unmarshal(e.Value(), &src) != nil {
						continue
					}
					u.Source = &src
				}
			}
			select {
			case out <- u:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func isWrongSequence(err error) bool {
	var apiErr *jetstream.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
}

func (b jsBooks) LeaseCreate(ctx context.Context, key string, l Lease) (uint64, error) {
	kv, err := LeasesKV(ctx, b.js)
	if err != nil {
		return 0, err
	}
	raw, _ := json.Marshal(l)
	rev, err := kv.Create(ctx, key, raw)
	if errors.Is(err, jetstream.ErrKeyExists) || isWrongSequence(err) {
		return 0, ErrLeaseTaken
	}
	return rev, err
}

func (b jsBooks) LeaseUpdate(ctx context.Context, key string, l Lease, rev uint64) (uint64, error) {
	kv, err := LeasesKV(ctx, b.js)
	if err != nil {
		return 0, err
	}
	raw, _ := json.Marshal(l)
	next, err := kv.Update(ctx, key, raw, rev)
	if errors.Is(err, jetstream.ErrKeyNotFound) || isWrongSequence(err) {
		return 0, ErrLeaseLost
	}
	return next, err
}

func (b jsBooks) LeaseDelete(ctx context.Context, key string, rev uint64) error {
	kv, err := LeasesKV(ctx, b.js)
	if err != nil {
		return err
	}
	err = kv.Delete(ctx, key, jetstream.LastRevision(rev))
	if errors.Is(err, jetstream.ErrKeyNotFound) || isWrongSequence(err) {
		return nil
	}
	return err
}

func (b jsBooks) Cursor(ctx context.Context, key string) (Cursor, bool, error) {
	kv, err := CursorsKV(ctx, b.js)
	if err != nil {
		return Cursor{}, false, err
	}
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return Cursor{}, false, nil
	}
	if err != nil {
		return Cursor{}, false, err
	}
	var c Cursor
	if json.Unmarshal(e.Value(), &c) != nil {
		return Cursor{}, false, nil
	}
	return c, true, nil
}

func (b jsBooks) PutCursor(ctx context.Context, key string, c Cursor) error {
	kv, err := CursorsKV(ctx, b.js)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(c)
	_, err = kv.Put(ctx, key, raw)
	return err
}

func (b jsBooks) Publish(ctx context.Context, tenant uint64, connection, contract, event, id string, payload json.RawMessage, at time.Time) error {
	return PublishEvent(ctx, b.js, tenant, connection, contract, event, id, payload, at)
}

// InstanceKey reports whether an announcement key is a type's
// (<type>.<instance>), not a surface's.
func InstanceKey(k string) bool {
	typ, _, ok := strings.Cut(k, ".")
	return ok && typ != "surface"
}
