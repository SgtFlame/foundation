package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Reply is a call's reply body: the result, or a typed error. Secrets
// never appear in a request or a reply.
type Reply struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// CallHeaders are a call request's headers; its body is the operation's
// args.
type CallHeaders struct {
	CallID    string
	Contract  string // id@version
	Principal features.PrincipalRef
	Meta      features.Meta
	Trace     string
	Deadline  time.Time
	Key       string // the idempotency key, when the operation has one
}

// Set writes the headers onto a request.
func (h CallHeaders) Set(hdr nats.Header) {
	hdr.Set(HeaderCall, h.CallID)
	hdr.Set(HeaderContract, h.Contract)
	p, _ := json.Marshal(h.Principal)
	hdr.Set(HeaderPrincipal, string(p))
	m, _ := json.Marshal(h.Meta)
	hdr.Set(HeaderMeta, string(m))
	if h.Trace != "" {
		hdr.Set(HeaderTrace, h.Trace)
	}
	if !h.Deadline.IsZero() {
		hdr.Set(HeaderDeadline, h.Deadline.UTC().Format(time.RFC3339Nano))
	}
	if h.Key != "" {
		hdr.Set(HeaderKey, h.Key)
	}
}

// ReadCallHeaders reads a request's headers.
func ReadCallHeaders(hdr nats.Header) (CallHeaders, error) {
	h := CallHeaders{CallID: hdr.Get(HeaderCall), Contract: hdr.Get(HeaderContract), Trace: hdr.Get(HeaderTrace), Key: hdr.Get(HeaderKey)}
	if h.CallID == "" || h.Contract == "" {
		return h, fmt.Errorf("the request has no call id or contract")
	}
	if err := json.Unmarshal([]byte(hdr.Get(HeaderPrincipal)), &h.Principal); err != nil {
		return h, fmt.Errorf("the request's principal cannot be read: %v", err)
	}
	if m := hdr.Get(HeaderMeta); m != "" {
		_ = json.Unmarshal([]byte(m), &h.Meta)
	}
	if d := hdr.Get(HeaderDeadline); d != "" {
		t, err := time.Parse(time.RFC3339Nano, d)
		if err != nil {
			return h, fmt.Errorf("the request's deadline %q cannot be read", d)
		}
		h.Deadline = t
	}
	return h, nil
}

// Announcement is what an adapter instance writes to cx_adapters for
// each type and surface it serves, and refreshes every few seconds. A
// type is online while one of its instances' announcements has not
// expired.
type Announcement struct {
	Adapter  string `json:"adapter"`  // the adapter's name, such as forge or mail-adapter
	Instance string `json:"instance"` // this instance, unique
	// Type, with the Contracts it names, or Surface: one of them.
	Type      *Type        `json:"type,omitempty"`
	Contracts []Contract   `json:"contracts,omitempty"`
	Surface   *SurfaceInfo `json:"surface,omitempty"`
	// Tenants it serves; every tenant when empty.
	Tenants   []uint64  `json:"tenants,omitempty"`
	At        time.Time `json:"at"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Key is the announcement's key: <type>.<instance> or
// surface.<surface>.<instance>.
func (a Announcement) Key() string {
	if a.Surface != nil {
		return "surface." + Token(a.Surface.ID) + "." + a.Instance
	}
	if a.Type != nil {
		return Token(a.Type.ID) + "." + a.Instance
	}
	return "_." + a.Instance
}

// Serves reports whether the announcement serves tenant.
func (a Announcement) Serves(tenant uint64) bool {
	return len(a.Tenants) == 0 || slices.Contains(a.Tenants, tenant)
}

// Online reports whether the announcement has not expired at now.
func (a Announcement) Online(now time.Time) bool { return now.Before(a.ExpiresAt) }

// keepAnnouncements is how long the bucket keeps an entry no instance
// refreshes, after which it is gone rather than offline.
const keepAnnouncements = 10 * time.Minute

// AdaptersKV is the cx_adapters bucket, made on first use.
func AdaptersKV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, AdaptersBucket)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: AdaptersBucket, History: 1, TTL: keepAnnouncements,
			Description: "Connector adapters' announcements: <type>.<instance>, surface.<surface>.<instance> (forge 07b)"})
		if errors.Is(err, jetstream.ErrBucketExists) {
			kv, err = js.KeyValue(ctx, AdaptersBucket)
		}
	}
	return kv, err
}

// Announce writes an announcement.
func Announce(ctx context.Context, kv jetstream.KeyValue, a Announcement) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, a.Key(), raw)
	return err
}

// Discovered is a connection's discovered contract as cx_discovered_<tenant>
// keeps it (15c): the contract, and the operations left out of it, which
// failed the metadata check.
type Discovered struct {
	Contract Contract  `json:"contract"`
	Problems []Problem `json:"problems,omitempty"`
	At       time.Time `json:"at"`
}

func discoveredKV(ctx context.Context, js jetstream.JetStream, tenant uint64, create bool) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, DiscoveredBucket(tenant))
	if errors.Is(err, jetstream.ErrBucketNotFound) && create {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: DiscoveredBucket(tenant), History: 1,
			Description: "Discovered connector contracts, by connection (forge 07b, 15c)"})
		if errors.Is(err, jetstream.ErrBucketExists) {
			kv, err = js.KeyValue(ctx, DiscoveredBucket(tenant))
		}
	}
	return kv, err
}

// PutDiscovered stores a connection's discovered contract, as an adapter
// does through its Context: operations that fail the metadata check are
// left out and listed as problems. It publishes cx.discovered.<tenant>.<connection>.
func PutDiscovered(ctx context.Context, js jetstream.JetStream, tenant uint64, connection string, c Contract) (Discovered, error) {
	c.ID, c.Version = DiscoveredID(connection), max(c.Version, 1)
	d := FilterDiscovered(c)
	kv, err := discoveredKV(ctx, js, tenant, true)
	if err != nil {
		return d, err
	}
	raw, _ := json.Marshal(d)
	if _, err := kv.Put(ctx, connection, raw); err != nil {
		return d, err
	}
	return d, js.Conn().Publish(DiscoveredSubject(tenant, connection), nil)
}

// FilterDiscovered keeps the operations and events of a discovered
// contract that pass the check, listing the rest as problems.
func FilterDiscovered(c Contract) Discovered {
	d := Discovered{At: time.Now().UTC()}
	keep := c
	keep.Operations, keep.Events = nil, nil
	meta := &checker{}
	meta.meta("meta", c.Meta)
	d.Problems = meta.out
	for _, op := range c.Operations {
		one := Contract{ID: c.ID, Version: c.Version, Meta: c.Meta, Operations: []Operation{op}, Events: c.Events}
		if p := CheckContract(one); len(p) > 0 {
			d.Problems = append(d.Problems, p...)
			continue
		}
		keep.Operations = append(keep.Operations, op)
	}
	for _, e := range c.Events {
		one := Contract{ID: c.ID, Version: c.Version, Meta: c.Meta, Events: []EventDef{e}}
		if p := CheckContract(one); len(p) > 0 {
			d.Problems = append(d.Problems, p...)
			continue
		}
		keep.Events = append(keep.Events, e)
	}
	d.Contract = keep
	return d
}

// GetDiscovered reads a connection's discovered contract.
func GetDiscovered(ctx context.Context, js jetstream.JetStream, tenant uint64, connection string) (Discovered, bool, error) {
	kv, err := discoveredKV(ctx, js, tenant, false)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		return Discovered{}, false, nil
	}
	if err != nil {
		return Discovered{}, false, err
	}
	e, err := kv.Get(ctx, connection)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return Discovered{}, false, nil
	}
	if err != nil {
		return Discovered{}, false, err
	}
	var d Discovered
	if err := json.Unmarshal(e.Value(), &d); err != nil {
		return Discovered{}, false, err
	}
	return d, true, nil
}
