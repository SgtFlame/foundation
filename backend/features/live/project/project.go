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
	"slices"
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
// forever (01-ServerUI question 6), and a command's several events are
// published as one atomic batch (forge 03h).
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
		Retention: jetstream.LimitsPolicy, Duplicates: Duplicates, AllowDirect: true, AllowAtomicPublish: true,
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
	if err := p.allowAtomic(ctx, "KV_"+name); err != nil {
		return nil, err
	}
	if err := p.run(ctx, tenant, pr, stream, kv); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.buckets[name] = kv
	p.mu.Unlock()
	return kv, nil
}

// allowAtomic lets a bucket's stream take atomic batches, so a batch of
// events is written to a projection all at once.
func (p *Projections) allowAtomic(ctx context.Context, stream string) error {
	s, err := p.js.Stream(ctx, stream)
	if err != nil {
		return log.Wrapf(err, "cannot read the stream %s", stream)
	}
	cfg := s.CachedInfo().Config
	if cfg.AllowAtomicPublish {
		return nil
	}
	cfg.AllowAtomicPublish = true
	if _, err := p.js.UpdateStream(ctx, cfg); err != nil {
		return log.Wrapf(err, "cannot allow atomic batches on %s", stream)
	}
	return nil
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
		// Long work (rebuilding an index) is not redelivered while it runs
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					_ = msg.InProgress()
				}
			}
		}()
		err = fn(e)
		close(done)
		if err != nil {
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

// History reads a target's events in order, from the first, passing each
// to fn until it returns false or the events committed so far are read:
// what a command reads when it needs an earlier state of its target (an
// app published at the rev its publisher saw).
func (p *Projections) History(ctx context.Context, tenant uint64, t live.Target, fn func(live.Event) bool) error {
	stream, err := p.Stream(ctx, tenant, t.Area)
	if err != nil {
		return err
	}
	last, err := stream.GetLastMsgForSubject(ctx, t.Subjects(tenant))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil
	}
	if err != nil {
		return log.Wrapf(err, "cannot read the history of %s", t.Key())
	}
	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{FilterSubjects: []string{t.Subjects(tenant)}})
	if err != nil {
		return log.Wrapf(err, "cannot read the history of %s", t.Key())
	}
	for {
		msg, err := cons.Next(jetstream.FetchMaxWait(5 * time.Second))
		if err != nil {
			return log.Wrapf(err, "cannot read the history of %s", t.Key())
		}
		meta, err := msg.Metadata()
		if err != nil {
			return err
		}
		var e live.Event
		if err := json.Unmarshal(msg.Data(), &e); err != nil {
			return log.Wrapf(err, "cannot read an event of %s", t.Key())
		}
		e.Stream, e.Seq = meta.Stream, meta.Sequence.Stream
		if !fn(e) || e.Seq >= last.Sequence {
			return nil
		}
	}
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
	events := []live.Event{e}
	if e.Batch != nil && e.Batch.Index < e.Batch.Count {
		// The rest of the batch is folded now and written with this event,
		// so no read of the projection sees half a batch. Its later events
		// are then skipped as already folded.
		if events, err = p.batchFrom(ctx, pr, e); err != nil {
			log.Error(ctx, err, "projector %s: cannot read the batch of event %d", pr.ID, e.Seq)
			_ = msg.NakWithDelay(time.Second)
			return
		}
	}
	// Each key's old value and revision, and its new value, in the order
	// the events first touched them
	type change struct {
		key      string
		old, new live.Projected
		rev      uint64
	}
	var changes []*change
	byKey := map[string]*change{}
	for _, e := range events {
		key := e.Target.Key()
		if pr.KeyOf != nil {
			if key = pr.KeyOf(e); key == "" {
				continue
			}
		}
		c := byKey[key]
		if c == nil {
			old, rev, err := getRev(ctx, kv, key)
			if err != nil {
				log.Error(ctx, err, "projector %s: cannot read %s", pr.ID, key)
				_ = msg.NakWithDelay(time.Second)
				return
			}
			if old.Seq >= e.Seq { // already folded: projectors are idempotent
				continue
			}
			c = &change{key: key, old: old, new: old, rev: rev}
			byKey[key] = c
			changes = append(changes, c)
		}
		value, err := pr.Fold(live.Fold{Key: key, State: c.new.Value, Event: e})
		if err != nil {
			log.With("subject", msg.Subject()).Error(ctx, err, "projector %s cannot fold event %d; skipped", pr.ID, e.Seq)
			_ = msg.Term()
			return
		}
		// A deleted key keeps its sequence, so a redelivered event is not
		// folded again
		c.new = live.Projected{Stream: e.Stream, Seq: e.Seq, Value: value}
	}
	writes := make([]kvWrite, len(changes))
	for i, c := range changes {
		if pr.Keep != nil {
			kept, err := pr.Keep(live.Keep{Ctx: ctx, Tenant: e.TenantID, Key: c.key, Value: c.new.Value, Kept: c.old.Kept})
			if err != nil {
				log.With("subject", msg.Subject()).Error(ctx, err, "projector %s cannot keep the keys of %s; retrying", pr.ID, c.key)
				_ = msg.NakWithDelay(time.Second)
				return
			}
			c.new.Kept = kept
		}
		raw, _ := json.Marshal(c.new)
		writes[i] = kvWrite{key: c.key, raw: raw, rev: c.rev}
	}
	// The writes expect the revisions read, so a value changed meanwhile
	// (by Rekeep) is folded again from the new one.
	switch {
	case len(writes) == 1:
		err = write(ctx, kv, writes[0].key, writes[0].raw, writes[0].rev)
	case len(writes) > 1:
		err = writeBatch(ctx, p.js.Conn(), kv.Bucket(), writes)
	}
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			_ = msg.Nak()
			return
		}
		log.Error(ctx, err, "projector %s: cannot write %d values", pr.ID, len(writes))
		_ = msg.NakWithDelay(time.Second)
		return
	}
	_ = msg.Ack()
}

// batchFrom is e and the events after it in its batch that the projector
// folds, read from the stream: a batch is stored in one run.
func (p *Projections) batchFrom(ctx context.Context, pr live.Projector, e live.Event) ([]live.Event, error) {
	stream, err := p.Stream(ctx, e.TenantID, pr.Area)
	if err != nil {
		return nil, err
	}
	_, last := e.Batch.Range(e.Seq)
	events := []live.Event{e}
	for seq := e.Seq + 1; seq <= last; seq++ {
		m, err := stream.GetMsg(ctx, seq)
		if err != nil {
			return nil, log.Wrap(err)
		}
		_, t, _, ok := live.TargetOf(m.Subject)
		if !ok || (len(pr.Types) > 0 && !slices.Contains(pr.Types, t.Type)) {
			continue
		}
		var next live.Event
		if err := json.Unmarshal(m.Data, &next); err != nil {
			return nil, log.Wrapf(err, "cannot read event %d", seq)
		}
		next.Stream, next.Seq = e.Stream, seq
		events = append(events, next)
	}
	return events, nil
}

// write puts a value that was at revision rev (0: there was none).
func write(ctx context.Context, kv jetstream.KeyValue, key string, raw []byte, rev uint64) error {
	var err error
	if rev == 0 {
		_, err = kv.Create(ctx, key, raw)
	} else {
		_, err = kv.Update(ctx, key, raw, rev)
	}
	return err
}

func get(ctx context.Context, kv jetstream.KeyValue, key string) (live.Projected, bool, error) {
	v, _, err := getRev(ctx, kv, key)
	return v, err == nil && v.Value != nil, err
}

// getRev is a key's value and its revision, 0 when it has none.
func getRev(ctx context.Context, kv jetstream.KeyValue, key string) (live.Projected, uint64, error) {
	entry, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return live.Projected{}, 0, nil
	}
	if err != nil {
		return live.Projected{}, 0, err
	}
	v, err := decode(entry)
	return v, entry.Revision(), err
}

// Rekeep runs fn as a projector's Keep again for one key, outside a fold,
// when what a value keeps has changed (an index was added): fn gets the
// value and the keys kept for it, and returns the keys to keep instead.
// They are stored unless the value changed meanwhile, when fn runs again.
// A key without a value is left alone.
func (p *Projections) Rekeep(ctx context.Context, tenant uint64, projector, key string, fn func(value json.RawMessage, kept []string) ([]string, error)) error {
	kv, err := p.Bucket(ctx, tenant, projector)
	if err != nil {
		return err
	}
	for {
		v, rev, err := getRev(ctx, kv, key)
		if err != nil {
			return log.Wrap(err)
		}
		if v.Value == nil {
			return nil
		}
		kept, err := fn(v.Value, v.Kept)
		if err != nil {
			return err
		}
		v.Kept = kept
		raw, _ := json.Marshal(v)
		err = write(ctx, kv, key, raw, rev)
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return err
		}
	}
}

// Side is a bucket kept beside the projections, PRJ_<tenant>_<name> (dots
// as '_'), made on first use: where a projector's Keep keeps its keys.
func (p *Projections) Side(ctx context.Context, tenant uint64, name string) (jetstream.KeyValue, error) {
	bucket := live.Bucket(tenant, name)
	p.mu.Lock()
	kv := p.buckets[bucket]
	p.mu.Unlock()
	if kv != nil {
		return kv, nil
	}
	kv, err := p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket, History: 1, Storage: jetstream.FileStorage})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the bucket %s", bucket)
	}
	p.mu.Lock()
	p.buckets[bucket] = kv
	p.mu.Unlock()
	return kv, nil
}

// Settle waits until the projector has folded every event its stream
// held when Settle was called, or ctx ends.
func (p *Projections) Settle(ctx context.Context, tenant uint64, projector string) error {
	pr, ok := p.Projector(projector)
	if !ok {
		return log.Code("projection_unknown").Errorf("no projector %q", projector)
	}
	if _, err := p.Bucket(ctx, tenant, projector); err != nil {
		return err
	}
	stream, err := p.Stream(ctx, tenant, pr.Area)
	if err != nil {
		return err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return log.Wrap(err)
	}
	last := info.State.LastSeq
	cons, err := stream.Consumer(ctx, "prj_"+strings.ReplaceAll(pr.ID, ".", "_"))
	if err != nil {
		return log.Wrap(err)
	}
	for {
		ci, err := cons.Info(ctx)
		if err != nil {
			return log.Wrap(err)
		}
		if ci.AckFloor.Stream >= last || (ci.NumPending == 0 && ci.NumAckPending == 0) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Folded is how far the projector has folded its stream: every event at
// or below it is in the projection. A read that starts after it reflects
// at least that position.
func (p *Projections) Folded(ctx context.Context, tenant uint64, projector string) (live.Position, error) {
	pr, ok := p.Projector(projector)
	if !ok {
		return nil, log.Code("projection_unknown").Errorf("no projector %q", projector)
	}
	if _, err := p.Bucket(ctx, tenant, projector); err != nil {
		return nil, err
	}
	stream, err := p.Stream(ctx, tenant, pr.Area)
	if err != nil {
		return nil, err
	}
	cons, err := stream.Consumer(ctx, "prj_"+strings.ReplaceAll(pr.ID, ".", "_"))
	if err != nil {
		return nil, log.Wrap(err)
	}
	ci, err := cons.Info(ctx)
	if err != nil {
		return nil, log.Wrap(err)
	}
	return live.Position{live.StreamName(tenant, pr.Area): ci.AckFloor.Stream}, nil
}

// Context ends when the projections close: the lifetime of work that
// follows them, such as an index held in memory.
func (p *Projections) Context() context.Context { return p.ctx }

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
	return p.watch(ctx, tenant, projector, pattern, false)
}

// Updates sends each change to the keys matching pattern from now on, but
// not the keys as they are, until ctx ends: for a query that reads its
// snapshot another way, such as from an index.
func (p *Projections) Updates(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error) {
	return p.watch(ctx, tenant, projector, pattern, true)
}

func (p *Projections) watch(ctx context.Context, tenant uint64, projector, pattern string, updates bool) (<-chan *live.Entry, error) {
	kv, err := p.Bucket(ctx, tenant, projector)
	if err != nil {
		return nil, err
	}
	var opts []jetstream.WatchOpt
	if updates {
		opts = append(opts, jetstream.UpdatesOnly())
	}
	w, err := kv.Watch(ctx, pattern, opts...)
	if err != nil {
		return nil, log.Wrap(err)
	}
	out := make(chan *live.Entry, 64)
	go func() {
		defer close(out)
		defer func() { _ = w.Stop() }()
		initial := !updates
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
