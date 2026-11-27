// Package ctl is what the daemons and the control plane share
// (forge 20b-2, 20c-1): the control account's buckets and stream, control
// requests and enrollment. It imports little, so axiomd stays small.
package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The buckets and the stream in the control account
// (forge 20b-2): desired state resolved per target, reported facts and
// health, and the CTL stream of control requests' progress.
const (
	DesiredBucket = "ctl_desired"      // host.<id>, inst.<id>: the resolved desired state
	FactsBucket   = "ctl_facts"        // host.<id>, inst.<id>: facts, written on start and on change
	HealthBucket  = "ctl_health"       // host.<id>, inst.<id>: heartbeats, gone after HealthTTL
	RolloutBucket = "ctl_nats_rollout" // <cluster>: the NATS node changing now, gone after RolloutTTL
	RepoBucket    = "ctl_repo"         // object store: the first-party apt repository (forge 20f-1)
	StreamName    = "CTL"              // ctl.evt.<host|inst>.<id>.<event>
)

// HealthTTL is how long a heartbeat counts: a daemon beats more often.
// Tests shorten it before anything is made.
var HealthTTL = 30 * time.Second

// RepoPort is where `axiomd repo-serve` serves the first-party apt
// repository on every NATS node's instance (forge 20f-1).
const RepoPort = 8090

// RolloutTTL is how long a NATS cluster's rollout lease lives unrefreshed
// (forge 20e): a crashed agent's lease frees itself.
var RolloutTTL = 2 * time.Minute

// Target kinds: a host's daemon or an instance's.
const (
	KindHost     = "host"
	KindInstance = "inst"
)

// Target is a daemon's: its kind and its record's id.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Key is its key in the control buckets: host.<id> or inst.<id>.
func (t Target) Key() string { return t.Kind + "." + t.ID }

// RequestSubject is where its daemon answers a verb: ctl.<kind>.<id>.<verb>.
func (t Target) RequestSubject(verb string) string {
	return "ctl." + t.Kind + "." + t.ID + "." + verb
}

// Requests is every request subject of its daemon, which a daemon's user
// may subscribe to and nothing else (20c-1).
func (t Target) Requests() string { return "ctl." + t.Kind + "." + t.ID + ".>" }

// EventSubject is where its daemon reports a request's progress.
func (t Target) EventSubject(kind string) string {
	return "ctl.evt." + t.Kind + "." + t.ID + "." + kind
}

// TargetOfKey reads a bucket key back.
func TargetOfKey(key string) (Target, bool) {
	k, id, ok := strings.Cut(key, ".")
	if !ok || (k != KindHost && k != KindInstance) || id == "" {
		return Target{}, false
	}
	return Target{Kind: k, ID: id}, true
}

// Request is a control request: its id is its idempotency key (the
// Nats-Msg-Id of its events on CTL), actor who asked (user:<id>, chat,
// agent:<id>, controlplane), request the operation log's request.
type Request struct {
	ID      string          `json:"id"`
	Verb    string          `json:"verb"`
	Args    json.RawMessage `json:"args,omitempty"`
	Actor   string          `json:"actor,omitempty"`
	Request string          `json:"request,omitempty"`
}

// Reply is a daemon's answer, given at once: the work's outcome arrives
// on CTL.
type Reply struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// Event kinds on CTL.
const (
	EventAccepted = "accepted"
	EventProgress = "progress"
	EventDone     = "done"
	EventFailed   = "failed"
)

// Event is a request's progress or outcome on CTL.
type Event struct {
	Request string          `json:"request"`
	Kind    string          `json:"kind"`
	Target  Target          `json:"target"`
	Verb    string          `json:"verb,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Seq     int             `json:"seq,omitempty"` // a progress event's number
	At      time.Time       `json:"at"`
}

// Health is a heartbeat: the daemon's own state and each unit's.
type Health struct {
	At     time.Time         `json:"at"`
	Status string            `json:"status"` // up
	Units  map[string]string `json:"units,omitempty"`
}

// Control is the control account's JetStream and connection.
type Control struct {
	NC *nats.Conn
	JS jetstream.JetStream
}

// Ensure makes the buckets and the stream if they do not exist.
func (c *Control) Ensure(ctx context.Context) error {
	if err := c.ensureKV(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: DesiredBucket, Description: "Each daemon's resolved desired state", History: 1, Storage: jetstream.FileStorage})); err != nil {
		return err
	}
	if err := c.ensureKV(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: FactsBucket, Description: "What each daemon reports about itself", History: 1, Storage: jetstream.FileStorage})); err != nil {
		return err
	}
	if err := c.ensureKV(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: HealthBucket, Description: "Each daemon's heartbeat", History: 1, TTL: HealthTTL, Storage: jetstream.MemoryStorage})); err != nil {
		return err
	}
	if err := c.ensureKV(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: RolloutBucket, Description: "Each NATS cluster's rollout lease: the one node changing now (forge 20e)", History: 1, TTL: RolloutTTL, Storage: jetstream.FileStorage})); err != nil {
		return err
	}
	_, err := c.JS.CreateOrUpdateStream(ctx, natsd.Stream(jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{"ctl.evt.>"}, Storage: jetstream.FileStorage,
		MaxAge: 30 * 24 * time.Hour, Duplicates: 24 * time.Hour,
	}))
	return log.Wrap(err)
}

func (c *Control) ensureKV(ctx context.Context, cfg jetstream.KeyValueConfig) error {
	_, err := c.JS.CreateOrUpdateKeyValue(ctx, cfg)
	return log.Wrapf(err, "cannot make the bucket %s", cfg.Bucket)
}

func (c *Control) KV(ctx context.Context, name string) (jetstream.KeyValue, error) {
	kv, err := c.JS.KeyValue(ctx, name)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		if err := c.Ensure(ctx); err != nil {
			return nil, err
		}
		kv, err = c.JS.KeyValue(ctx, name)
	}
	return kv, log.Wrap(err)
}

// Desired reads a target's resolved desired state; false when it has none.
func (c *Control) Desired(ctx context.Context, t Target) (json.RawMessage, bool, error) {
	return c.get(ctx, DesiredBucket, t)
}

// Facts reads a target's reported facts.
func (c *Control) Facts(ctx context.Context, t Target) (json.RawMessage, bool, error) {
	return c.get(ctx, FactsBucket, t)
}

// Health reads a target's last heartbeat; false once it has expired.
func (c *Control) Health(ctx context.Context, t Target) (Health, bool, error) {
	raw, ok, err := c.get(ctx, HealthBucket, t)
	var h Health
	if ok {
		_ = json.Unmarshal(raw, &h)
		ok = time.Since(h.At) < HealthTTL
	}
	return h, ok, err
}

func (c *Control) get(ctx context.Context, bucket string, t Target) (json.RawMessage, bool, error) {
	kv, err := c.KV(ctx, bucket)
	if err != nil {
		return nil, false, err
	}
	e, err := kv.Get(ctx, t.Key())
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, log.Wrap(err)
	}
	return e.Value(), true, nil
}

// PutDesired writes a target's desired state when it changed, and tells
// whether it did.
func (c *Control) PutDesired(ctx context.Context, t Target, v json.RawMessage) (bool, error) {
	old, ok, err := c.Desired(ctx, t)
	if err != nil {
		return false, err
	}
	if ok && string(old) == string(v) {
		return false, nil
	}
	kv, err := c.KV(ctx, DesiredBucket)
	if err != nil {
		return false, err
	}
	_, err = kv.Put(ctx, t.Key(), v)
	return err == nil, log.Wrap(err)
}

// DeleteDesired removes a target's desired state (its record is gone).
func (c *Control) DeleteDesired(ctx context.Context, t Target) error {
	kv, err := c.KV(ctx, DesiredBucket)
	if err != nil {
		return err
	}
	err = kv.Delete(ctx, t.Key())
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil
	}
	return log.Wrap(err)
}

// PutFacts writes a target's facts (a daemon's).
func (c *Control) PutFacts(ctx context.Context, t Target, facts any) error {
	kv, err := c.KV(ctx, FactsBucket)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(facts)
	_, err = kv.Put(ctx, t.Key(), raw)
	return log.Wrap(err)
}

// Beat writes a target's heartbeat (a daemon's).
func (c *Control) Beat(ctx context.Context, t Target, h Health) error {
	kv, err := c.KV(ctx, HealthBucket)
	if err != nil {
		return err
	}
	if h.At.IsZero() {
		h.At = time.Now().UTC()
	}
	if h.Status == "" {
		h.Status = "up"
	}
	raw, _ := json.Marshal(h)
	_, err = kv.Put(ctx, t.Key(), raw)
	return log.Wrap(err)
}

// Send sends a control request to a target's daemon and returns its
// reply; no daemon listening is ErrNoDaemon.
func (c *Control) Send(ctx context.Context, t Target, r Request) (Reply, error) {
	raw, _ := json.Marshal(r)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	m, err := c.NC.RequestWithContext(ctx, t.RequestSubject(r.Verb), raw)
	if errors.Is(err, nats.ErrNoResponders) {
		return Reply{}, ErrNoDaemon
	}
	if err != nil {
		return Reply{}, log.Wrap(err)
	}
	var rep Reply
	if err := json.Unmarshal(m.Data, &rep); err != nil {
		return Reply{}, log.Wrap(err)
	}
	return rep, nil
}

// ErrNoDaemon is a request no daemon answered.
var ErrNoDaemon = log.Code("no_daemon").New("no daemon answers for the target")

// Report publishes a request's progress or outcome on CTL, once: the same
// request, kind and progress number are one message however often sent.
func (c *Control) Report(ctx context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	raw, _ := json.Marshal(e)
	id := fmt.Sprintf("%s/%s/%d", e.Request, e.Kind, e.Seq)
	_, err := c.JS.Publish(ctx, e.Target.EventSubject(e.Kind), raw, jetstream.WithMsgID(id))
	return log.Wrap(err)
}
