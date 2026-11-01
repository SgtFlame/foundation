// Package project keeps the live core's streams and projections: one event
// stream per tenant and area, made when first used, and one KV bucket per
// tenant and projector, kept current by a durable consumer
// (documentation/Live.md › Projections). It is the live.Store queries read.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

var log = live.Log.Tags("project")

// Duplicates is how long the streams drop a command resent with the same id.
const Duplicates = 10 * time.Minute

// Projections is the live core's streams and projection buckets.
type Projections struct {
	js       jetstream.JetStream
	reg      *extensions.Registry
	mu       sync.Mutex
	streams  map[string]jetstream.Stream
	buckets  map[string]jetstream.KeyValue
	running  map[string]jetstream.ConsumeContext
	ctx      context.Context
	cancel   context.CancelFunc
	stopping bool
}

var _ live.Store = (*Projections)(nil)

func New(js jetstream.JetStream, reg *extensions.Registry) *Projections {
	ctx, cancel := context.WithCancel(context.Background())
	return &Projections{js: js, reg: reg, streams: map[string]jetstream.Stream{}, buckets: map[string]jetstream.KeyValue{},
		running: map[string]jetstream.ConsumeContext{}, ctx: ctx, cancel: cancel}
}

// JetStream is the connection the projections use.
func (p *Projections) JetStream() jetstream.JetStream { return p.js }

// Registry is the registry the projectors come from.
func (p *Projections) Registry() *extensions.Registry { return p.reg }

// Close stops every projector.
func (p *Projections) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopping = true
	for _, c := range p.running {
		c.Stop()
	}
	p.cancel()
}

// Stream makes sure the tenant's area stream exists. Events are kept
// forever (01-ServerUI question 6).
func (p *Projections) Stream(ctx context.Context, tenant uint64, area live.Area) (jetstream.Stream, error) {
	name := live.StreamName(tenant, area)
	p.mu.Lock()
	s := p.streams[name]
	p.mu.Unlock()
	if s != nil {
		return s, nil
	}
	if !live.ValidToken(string(area)) {
		return nil, log.Errorf("%q is not a valid area", area)
	}
	s, err := p.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: name, Subjects: []string{live.StreamSubjects(tenant, area)}, Storage: jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy, Duplicates: Duplicates, AllowDirect: true,
	})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the event stream %s", name)
	}
	p.mu.Lock()
	p.streams[name] = s
	p.mu.Unlock()
	return s, nil
}

// Projector finds a projector by id.
func (p *Projections) Projector(id string) (live.Projector, bool) {
	for _, pr := range live.Projectors.All(p.reg) {
		if pr.ID == id {
			return pr, true
		}
	}
	return live.Projector{}, false
}

// Bucket makes sure the tenant's projection exists and its projector runs.
func (p *Projections) Bucket(ctx context.Context, tenant uint64, projector string) (jetstream.KeyValue, error) {
	name := live.Bucket(tenant, projector)
	p.mu.Lock()
	kv := p.buckets[name]
	p.mu.Unlock()
	if kv != nil {
		return kv, nil
	}
	pr, ok := p.Projector(projector)
	if !ok {
		return nil, log.Code("projection_unknown").Errorf("no projector %q", projector)
	}
	stream, err := p.Stream(ctx, tenant, pr.Area)
	if err != nil {
		return nil, err
	}
	kv, err = p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, Description: pr.Title, History: 1, Storage: jetstream.FileStorage})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the projection %s", name)
	}
	if err := p.run(ctx, tenant, pr, stream, kv); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.buckets[name] = kv
	p.mu.Unlock()
	return kv, nil
}

// run starts the projector's durable consumer on the stream. MaxAckPending
// 1 keeps events strictly in order.
func (p *Projections) run(ctx context.Context, tenant uint64, pr live.Projector, stream jetstream.Stream, kv jetstream.KeyValue) error {
	key := fmt.Sprintf("%d/%s", tenant, pr.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[key] != nil || p.stopping {
		return nil
	}
	filters := []string{live.StreamSubjects(tenant, pr.Area)}
	if len(pr.Types) > 0 {
		filters = filters[:0]
		for _, t := range pr.Types {
			filters = append(filters, fmt.Sprintf("evt.%d.%s.%s.>", tenant, pr.Area, t))
		}
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "prj_" + strings.ReplaceAll(pr.ID, ".", "_"), FilterSubjects: filters,
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy, MaxAckPending: 1, AckWait: 30 * time.Second,
	})
	if err != nil {
		return log.Wrapf(err, "cannot make the consumer of projector %s", pr.ID)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) { p.fold(pr, kv, msg) })
	if err != nil {
		return log.Wrapf(err, "cannot start projector %s", pr.ID)
	}
	p.running[key] = cc
	return nil
}

// Follow runs fn on each event of the tenant's area whose subject matches
// one of subjects, in order, as the durable consumer fol_<name>: work an
// event causes outside the projections, such as copying it into SQLite.
// fn must be idempotent; an error retries the event a second later. Like
// a projector, a follower starts the first time it is asked for in a
// tenant, and catches up from where it stopped.
func (p *Projections) Follow(ctx context.Context, tenant uint64, area live.Area, name string, subjects []string, fn func(live.Event) error) error {
	key := fmt.Sprintf("%d/follow/%s", tenant, name)
	p.mu.Lock()
	started := p.running[key] != nil
	p.mu.Unlock()
	if started {
		return nil
	}
	stream, err := p.Stream(ctx, tenant, area)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[key] != nil || p.stopping {
		return nil
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "fol_" + strings.ReplaceAll(name, ".", "_"), FilterSubjects: subjects,
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy, MaxAckPending: 1, AckWait: 30 * time.Second,
	})
	if err != nil {
		return log.Wrapf(err, "cannot make the consumer of follower %s", name)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		meta, err := msg.Metadata()
		var e live.Event
		if err == nil {
			err = json.Unmarshal(msg.Data(), &e)
		}
		if err != nil {
			log.Error(p.ctx, err, "follower %s cannot read an event; skipped", name)
			_ = msg.Term()
			return
		}
		e.Stream, e.Seq = meta.Stream, meta.Sequence.Stream
		if err := fn(e); err != nil {
			log.With("subject", msg.Subject()).Error(p.ctx, err, "follower %s failed on event %d; retrying", name, e.Seq)
			_ = msg.NakWithDelay(time.Second)
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return log.Wrapf(err, "cannot start follower %s", name)
	}
	p.running[key] = cc
	return nil
}

func (p *Projections) fold(pr live.Projector, kv jetstream.KeyValue, msg jetstream.Msg) {
	ctx := p.ctx
	meta, err := msg.Metadata()
	if err != nil {
		log.Error(ctx, err, "projector %s: no metadata", pr.ID)
		_ = msg.Term()
		return
	}
	var e live.Event
	if err := json.Unmarshal(msg.Data(), &e); err != nil {
		log.Error(ctx, err, "projector %s: cannot read event %d", pr.ID, meta.Sequence.Stream)
		_ = msg.Term()
		return
	}
	e.Stream, e.Seq = meta.Stream, meta.Sequence.Stream
	key := e.Target.Key()
	old, _, err := get(ctx, kv, key)
	if err != nil {
		log.Error(ctx, err, "projector %s: cannot read %s", pr.ID, key)
		_ = msg.NakWithDelay(time.Second)
		return
	}
	if old.Seq >= e.Seq { // already folded: projectors are idempotent
		_ = msg.Ack()
		return
	}
	value, err := pr.Fold(live.Fold{Key: key, State: old.Value, Event: e})
	if err != nil {
		log.With("subject", msg.Subject()).Error(ctx, err, "projector %s cannot fold event %d; skipped", pr.ID, e.Seq)
		_ = msg.Term()
		return
	}
	// A deleted key keeps its sequence, so a redelivered event is not folded again
	raw, _ := json.Marshal(live.Projected{Stream: e.Stream, Seq: e.Seq, Value: value})
	if _, err := kv.Put(ctx, key, raw); err != nil {
		log.Error(ctx, err, "projector %s: cannot write %s", pr.ID, key)
		_ = msg.NakWithDelay(time.Second)
		return
	}
	_ = msg.Ack()
}

func get(ctx context.Context, kv jetstream.KeyValue, key string) (live.Projected, bool, error) {
	entry, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return live.Projected{}, false, nil
	}
	if err != nil {
		return live.Projected{}, false, err
	}
	v, err := decode(entry)
	return v, err == nil && v.Value != nil, err
}

func decode(entry jetstream.KeyValueEntry) (live.Projected, error) {
	var v live.Projected
	if entry.Operation() != jetstream.KeyValuePut {
		return v, nil
	}
	err := json.Unmarshal(entry.Value(), &v)
	if string(v.Value) == "null" {
		v.Value = nil
	}
	return v, err
}

// Get reads one projected value; ok is false when the key has none. Its
// Stream is named either way.
func (p *Projections) Get(ctx context.Context, tenant uint64, projector, key string) (live.Projected, bool, error) {
	kv, err := p.Bucket(ctx, tenant, projector)
	if err != nil {
		return live.Projected{}, false, err
	}
	v, ok, err := get(ctx, kv, key)
	if err != nil {
		return v, false, log.Wrap(err)
	}
	if v.Stream == "" {
		pr, _ := p.Projector(projector)
		v.Stream = live.StreamName(tenant, pr.Area)
	}
	return v, ok, nil
}

// WaitFor waits until the projected value reflects seq, or timeout, and
// returns it as it is then.
func (p *Projections) WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error) {
	v, _, err := p.Get(ctx, tenant, projector, key)
	if err != nil || v.Seq >= seq {
		return v, err
	}
	kv, _ := p.Bucket(ctx, tenant, projector)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	w, err := kv.Watch(ctx, key, jetstream.UpdatesOnly())
	if err != nil {
		return v, log.Wrap(err)
	}
	defer func() { _ = w.Stop() }()
	// The value may have changed before the watch started
	if v, _, err = p.Get(ctx, tenant, projector, key); err != nil || v.Seq >= seq {
		return v, err
	}
	for {
		select {
		case <-ctx.Done():
			return v, nil
		case entry, ok := <-w.Updates():
			if !ok {
				return v, nil
			}
			if entry == nil {
				continue
			}
			if nv, err := decode(entry); err == nil {
				nv.Stream = v.Stream
				v = nv
				if v.Seq >= seq {
					return v, nil
				}
			}
		}
	}
}

// Watch sends the keys matching pattern as they are, nil once, then each
// change, until ctx ends (live.Store).
func (p *Projections) Watch(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error) {
	kv, err := p.Bucket(ctx, tenant, projector)
	if err != nil {
		return nil, err
	}
	w, err := kv.Watch(ctx, pattern)
	if err != nil {
		return nil, log.Wrap(err)
	}
	out := make(chan *live.Entry, 64)
	go func() {
		defer close(out)
		defer func() { _ = w.Stop() }()
		initial := true
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-w.Updates():
				if !ok {
					return
				}
				var e *live.Entry
				if entry == nil {
					initial = false
				} else {
					v, err := decode(entry)
					if err != nil {
						continue
					}
					e = &live.Entry{Key: entry.Key(), Value: v, Deleted: v.Value == nil, Initial: initial}
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
