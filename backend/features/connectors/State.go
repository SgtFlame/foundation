package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The shared state of connections (forge 07c): every piece in JetStream,
// so adapters in any process, in any number, see the same.

// Subjects of connections served by Foundation in a queue group.
const (
	// ResolveQueue is the queue group serving cx.resolve and cx.status.
	ResolveQueue = "cx.foundation"
	// ResolveSubjects are every resolve request.
	ResolveSubjects = "cx.resolve.*.*"
	// StatusSubjects are every status report.
	StatusSubjects = "cx.status.*.*"
)

// ResolveSubject asks Foundation for a connection's settings and secrets:
// cx.resolve.<tenant>.<connection>.
func ResolveSubject(tenant uint64, connection string) string {
	return fmt.Sprintf("cx.resolve.%d.%s", tenant, connection)
}

// StatusSubject reports an event source's error on a connection, which
// Foundation commits as its status: cx.status.<tenant>.<connection>.
func StatusSubject(tenant uint64, connection string) string {
	return fmt.Sprintf("cx.status.%d.%s", tenant, connection)
}

// ResolveRequest is a cx.resolve body: the type the adapter serves, which
// must be the connection's.
type ResolveRequest struct {
	Type string `json:"type"`
}

// ResolveReply is a cx.resolve reply: the connection's settings with its
// secrets in place, or an error. It travels only to an adapter.
type ResolveReply struct {
	Type     string          `json:"type,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

// StatusReport is a cx.status body.
type StatusReport struct {
	Error Error `json:"error"`
}

// HeaderEventContract is a connector event's contract, id@version, on its
// message in CX_<tenant>.
const HeaderEventContract = "Cx-Contract"

// EventArea names CX_<tenant> where a trigger event's area is asked for.
const EventArea = "cx"

// StreamName is a tenant's connector event stream.
func StreamName(tenant uint64) string { return fmt.Sprintf("CX_%d", tenant) }

// EventDuplicates is CX_<tenant>'s duplicate window: it covers the longest
// gap between an event's publish and its cursor's write.
var EventDuplicates = 2 * time.Minute

// EventMaxAge is how long CX_<tenant> keeps events.
var EventMaxAge = 30 * 24 * time.Hour

// EventStream is the tenant's connector event stream, made on first use.
func EventStream(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.Stream, error) {
	name := StreamName(tenant)
	s, err := js.Stream(ctx, name)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil, err
	}
	return js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{fmt.Sprintf("cx.event.%d.>", tenant)},
		Description: "Connector events: cx.event.<tenant>.<connection>.<contract>.<event> (forge 07c)",
		Storage:     jetstream.FileStorage, Retention: jetstream.LimitsPolicy, Duplicates: EventDuplicates, MaxAge: EventMaxAge, AllowDirect: true})
}

// EventSubjectsOf are the subjects of a contract's event, of one
// connection or of every one.
func EventSubjectsOf(tenant uint64, connection, contractID, event string) string {
	if connection == "" {
		connection = "*"
	}
	return fmt.Sprintf("cx.event.%d.%s.%s.%s", tenant, connection, Token(contractID), Token(event))
}

// KV buckets. Sources, leases and cursors are one bucket each for every
// tenant, keyed <tenant>.<connection>, because an adapter instance watches
// every tenant's and a watch cannot span buckets.
const (
	SourcesBucket = "cx_sources"
	LeasesBucket  = "cx_leases"
	CursorsBucket = "cx_cursors"
)

// LedgerBucket is a tenant's ledger of idempotent operations.
func LedgerBucket(tenant uint64) string { return fmt.Sprintf("cx_ledger_%d", tenant) }

// LocksBucket is a tenant's locks on exclusive connections.
func LocksBucket(tenant uint64) string { return fmt.Sprintf("cx_locks_%d", tenant) }

// SecretsBucket is a tenant's connection secrets' placeholders.
func SecretsBucket(tenant uint64) string { return fmt.Sprintf("cx_secrets_%d", tenant) }

// SourceKey is a source, lease or cursor key.
func SourceKey(tenant uint64, connection string) string {
	return fmt.Sprintf("%d.%s", tenant, connection)
}

// ParseSourceKey splits a source key.
func ParseSourceKey(key string) (tenant uint64, connection string, ok bool) {
	t, c, found := strings.Cut(key, ".")
	if !found {
		return 0, "", false
	}
	if _, err := fmt.Sscan(t, &tenant); err != nil || tenant == 0 {
		return 0, "", false
	}
	return tenant, c, true
}

// Lifetimes.
var (
	// LedgerTTL is how long the ledger keeps what an outside service
	// accepted.
	LedgerTTL = 30 * 24 * time.Hour
	// LeaseTTL is how long a lease lasts unrefreshed.
	LeaseTTL = 15 * time.Second
	// LockTTL is how long an exclusive lock lasts when its holder dies.
	LockTTL = 2 * time.Minute
)

// Source is a cx_sources value: a connection whose events a trigger
// listens to, so an adapter instance serving its type runs its Watch.
type Source struct {
	Tenant       uint64    `json:"tenant,string"`
	Connection   string    `json:"connection"`
	Type         string    `json:"type"`
	Contracts    []string  `json:"contracts"` // id@version, those a trigger listens on
	PollInterval string    `json:"pollInterval,omitempty"`
	Since        string    `json:"since,omitempty"` // the initial window, a date
	Revision     uint64    `json:"revision"`        // the connection's, so a change restarts the source
	At           time.Time `json:"at"`
}

// Lease is a cx_leases value: who runs a source.
type Lease struct {
	Adapter     string    `json:"adapter"`
	Instance    string    `json:"instance"`
	Host        string    `json:"host"`
	Since       time.Time `json:"since"`
	LastError   string    `json:"lastError,omitempty"`
	LastErrorAt time.Time `json:"lastErrorAt,omitzero"`
}

// Cursor is a cx_cursors value: where a source resumes.
type Cursor struct {
	Cursor    json.RawMessage `json:"cursor"`
	At        time.Time       `json:"at"`
	LastEvent string          `json:"lastEvent,omitempty"` // <event>:<id>
}

// KV opens a bucket, making it on first use.
func KV(ctx context.Context, js jetstream.JetStream, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, cfg.Bucket)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		kv, err = js.CreateKeyValue(ctx, cfg)
		if errors.Is(err, jetstream.ErrBucketExists) {
			kv, err = js.KeyValue(ctx, cfg.Bucket)
		}
	}
	return kv, err
}

// SourcesKV is cx_sources.
func SourcesKV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	return KV(ctx, js, jetstream.KeyValueConfig{Bucket: SourcesBucket, History: 1,
		Description: "Connector event sources, kept by Foundation: <tenant>.<connection> (forge 07c)"})
}

// LeasesKV is cx_leases.
func LeasesKV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	return KV(ctx, js, jetstream.KeyValueConfig{Bucket: LeasesBucket, History: 1, TTL: LeaseTTL,
		Description: "Leases on connector event sources: <tenant>.<connection> (forge 07c)"})
}

// CursorsKV is cx_cursors.
func CursorsKV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	return KV(ctx, js, jetstream.KeyValueConfig{Bucket: CursorsBucket, History: 1,
		Description: "Connector event sources' cursors: <tenant>.<connection> (forge 07c)"})
}

// LedgerKey is a ledger entry's key: <connection>.<operation>.<hash of key>.
func LedgerKey(connection, op, key string) string {
	if connection == "" {
		connection = NoConnection
	}
	sum := sha256.Sum256([]byte(key))
	return connection + "." + Token(op) + "." + hex.EncodeToString(sum[:16])
}

// LedgerEntry is what the ledger holds: the result the outside service
// gave when it accepted the request.
type LedgerEntry struct {
	Result json.RawMessage `json:"result"`
	At     time.Time       `json:"at"`
	CallID string          `json:"callId,omitempty"`
}

func ledgerKV(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.KeyValue, error) {
	return KV(ctx, js, jetstream.KeyValueConfig{Bucket: LedgerBucket(tenant), History: 1, TTL: LedgerTTL,
		Description: "What outside services accepted, by connection, operation and idempotency key (forge 07c)"})
}

// LedgerGet reads a ledger entry.
func LedgerGet(ctx context.Context, js jetstream.JetStream, tenant uint64, connection, op, key string) (LedgerEntry, bool, error) {
	kv, err := ledgerKV(ctx, js, tenant)
	if err != nil {
		return LedgerEntry{}, false, err
	}
	e, err := kv.Get(ctx, LedgerKey(connection, op, key))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return LedgerEntry{}, false, nil
	}
	if err != nil {
		return LedgerEntry{}, false, err
	}
	var le LedgerEntry
	if err := json.Unmarshal(e.Value(), &le); err != nil {
		return LedgerEntry{}, false, err
	}
	return le, true, nil
}

// LedgerPut writes a ledger entry.
func LedgerPut(ctx context.Context, js jetstream.JetStream, tenant uint64, connection, op, key string, e LedgerEntry) error {
	kv, err := ledgerKV(ctx, js, tenant)
	if err != nil {
		return err
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	raw, _ := json.Marshal(e)
	_, err = kv.Put(ctx, LedgerKey(connection, op, key), raw)
	return err
}

// Lock takes the exclusive lock on a connection (a type whose service
// allows one session at a time), trying until ctx ends; release gives it
// back. A holder that dies loses it after LockTTL.
func Lock(ctx context.Context, js jetstream.JetStream, tenant uint64, connection, holder string) (release func(), err error) {
	kv, err := KV(ctx, js, jetstream.KeyValueConfig{Bucket: LocksBucket(tenant), History: 1, TTL: LockTTL,
		Description: "Locks on connections of exclusive types: <connection> (forge 07c)"})
	if err != nil {
		return nil, err
	}
	// Woken when the holder releases it; a lock whose holder died ends by
	// the bucket's TTL, which a watch does not see, so retry now and then
	var changes jetstream.KeyWatcher
	defer func() {
		if changes != nil {
			_ = changes.Stop()
		}
	}()
	for {
		rev, err := kv.Create(ctx, connection, []byte(holder))
		if err == nil {
			return func() {
				bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = kv.Delete(bg, connection, jetstream.LastRevision(rev))
			}, nil
		}
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return nil, err
		}
		if changes == nil {
			if changes, err = kv.Watch(ctx, connection, jetstream.UpdatesOnly()); err != nil {
				return nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, Errorf(CodeUnavailable, "connection %s is in use; try again", connection)
		case <-changes.Updates():
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// EventID is a connector event's id: its id fields' values joined by |,
// in the order the event names them, or a hash of the contract, event and
// payload when a field is missing (or the event names none).
func EventID(def EventDef, contract string, payload json.RawMessage) string {
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	var parts []string
	for _, f := range def.IDFields {
		v, ok := p[f]
		if !ok || v == nil || v == "" {
			parts = nil
			break
		}
		parts = append(parts, fmt.Sprint(v))
	}
	if len(parts) > 0 {
		return strings.Join(parts, "|")
	}
	sum := sha256.Sum256(append([]byte(contract+"|"+def.ID+"|"), payload...))
	return hex.EncodeToString(sum[:16])
}

// PublishEvent puts one connector event in CX_<tenant> on a connection,
// its id in the message id so an event published again (a poll after a
// crash, a trigger run twice) is kept once, and returns once JetStream has
// it. contract is id@version; an empty id is made from the payload.
func PublishEvent(ctx context.Context, js jetstream.JetStream, tenant uint64, connection, contract, event, id string, payload json.RawMessage, at time.Time) error {
	cid, version, err := ParseRef(contract)
	if err != nil || version == 0 {
		return Errorf(CodeInvalid, "event %s names no contract version: %q", event, contract)
	}
	if id == "" {
		sum := sha256.Sum256(append([]byte(contract+"|"+event+"|"), payload...))
		id = hex.EncodeToString(sum[:16])
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if _, err := EventStream(ctx, js, tenant); err != nil {
		return err
	}
	body, _ := json.Marshal(live.Event{Type: event, Target: live.Target{Area: EventArea, Type: Token(cid), ID: connection},
		Data: payload, CommandID: id, TenantID: tenant, Time: at})
	m := nats.NewMsg(EventSubject(tenant, connection, cid, event))
	m.Data = body
	m.Header.Set(jetstream.MsgIDHeader, connection+":"+event+":"+id)
	m.Header.Set(HeaderEventContract, contract)
	_, err = js.PublishMsg(ctx, m)
	return err
}
