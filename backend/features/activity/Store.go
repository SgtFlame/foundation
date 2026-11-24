package activity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/nats-io/nats.go/jetstream"
)

// Store keeps the activity streams in JetStream. Every write is
// idempotent by its entry's id (Nats-Msg-Id), so several runners writing
// the same entry store it once.
type Store struct {
	js   jetstream.JetStream
	mu   sync.Mutex
	made map[uint64]jetstream.Stream
}

// NewStore is a store over js.
func NewStore(js jetstream.JetStream) *Store {
	return &Store{js: js, made: map[uint64]jetstream.Stream{}}
}

// Stream is a tenant's activity stream, made on first use: kept at most
// a day and 1000 entries per person, each entry with its own time to live.
func (s *Store) Stream(ctx context.Context, tenant uint64) (jetstream.Stream, error) {
	s.mu.Lock()
	st := s.made[tenant]
	s.mu.Unlock()
	if st != nil {
		return st, nil
	}
	st, err := s.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: StreamName(tenant), Subjects: []string{Subjects(tenant)}, Storage: jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy, Discard: jetstream.DiscardOld, MaxAge: MaxHours * time.Hour, MaxMsgsPerSubject: MaxCount,
		Duplicates: project.Duplicates, AllowMsgTTL: true, AllowDirect: true,
	})
	if err != nil {
		return nil, log.Wrapf(err, "cannot make the activity stream %s", StreamName(tenant))
	}
	s.mu.Lock()
	s.made[tenant] = st
	s.mu.Unlock()
	return st, nil
}

// Append stores an entry on a person's subject for as long as their
// settings keep it, then drops their oldest entries past the count.
func (s *Store) Append(ctx context.Context, tenant, user uint64, e Entry, set Settings) error {
	st, err := s.Stream(ctx, tenant)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return log.Wrap(err)
	}
	subject := Subject(tenant, user)
	ack, err := s.js.Publish(ctx, subject, raw, jetstream.WithMsgID(e.ID), jetstream.WithMsgTTL(max(set.Keep(), time.Second)))
	if err != nil {
		return log.Wrapf(err, "cannot record activity on %s", subject)
	}
	if ack.Duplicate {
		return nil
	}
	err = st.Purge(ctx, jetstream.WithPurgeSubject(subject), jetstream.WithPurgeKeep(uint64(set.Count())))
	if err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
		return log.Wrapf(err, "cannot trim the activity on %s", subject)
	}
	return nil
}

// Purge deletes everything kept for a person.
func (s *Store) Purge(ctx context.Context, tenant, user uint64) error {
	st, err := s.Stream(ctx, tenant)
	if err != nil {
		return err
	}
	err = st.Purge(ctx, jetstream.WithPurgeSubject(Subject(tenant, user)))
	if err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
		return log.Wrapf(err, "cannot clear the activity of user %d", user)
	}
	return nil
}

// People are the people who have entries in a tenant's stream.
func (s *Store) People(ctx context.Context, tenant uint64) ([]uint64, error) {
	st, err := s.Stream(ctx, tenant)
	if err != nil {
		return nil, err
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(Subjects(tenant)))
	if err != nil {
		return nil, log.Wrap(err)
	}
	var out []uint64
	for subject := range info.State.Subjects {
		var t, u uint64
		if n, _ := sscanSubject(subject, &t, &u); n == 2 && t == tenant {
			out = append(out, u)
		}
	}
	return out, nil
}

// count is how many entries a person has now.
func (s *Store) count(ctx context.Context, st jetstream.Stream, subject string) (uint64, error) {
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(subject))
	if err != nil {
		return 0, log.Wrap(err)
	}
	return info.State.Subjects[subject], nil
}

// Follow sends a person's entries, oldest first, then each new one, until
// ctx ends; caught is called once after the entries kept when it started
// (at once when there are none). It returns when ctx ends or the stream
// fails.
func (s *Store) Follow(ctx context.Context, tenant, user uint64, entry func(Entry), caught func()) error {
	st, err := s.Stream(ctx, tenant)
	if err != nil {
		return err
	}
	subject := Subject(tenant, user)
	n, err := s.count(ctx, st, subject)
	if err != nil {
		return err
	}
	cons, err := st.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{FilterSubjects: []string{subject}, DeliverPolicy: jetstream.DeliverAllPolicy})
	if err != nil {
		return log.Wrap(err)
	}
	it, err := cons.Messages()
	if err != nil {
		return log.Wrap(err)
	}
	defer it.Stop()
	go func() {
		<-ctx.Done()
		it.Stop()
	}()
	if n == 0 {
		caught()
	}
	for {
		m, err := it.Next()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return nil
			}
			return log.Wrap(err)
		}
		var e Entry
		if json.Unmarshal(m.Data(), &e) == nil {
			entry(e)
		}
		if n > 0 {
			if meta, err := m.Metadata(); err == nil && meta.NumPending == 0 {
				n = 0
				caught()
			}
		}
	}
}

// Read is a person's entries kept now, oldest first.
func (s *Store) Read(ctx context.Context, tenant, user uint64) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out []Entry
	done := make(chan struct{})
	var once sync.Once
	errc := make(chan error, 1)
	go func() {
		errc <- s.Follow(ctx, tenant, user, func(e Entry) { out = append(out, e) }, func() { once.Do(func() { close(done) }) })
	}()
	select {
	case <-done:
		cancel()
		<-errc
		return out, nil
	case err := <-errc:
		if err == nil {
			err = log.Code("activity_unavailable").New("the activity could not be read in time")
		}
		return nil, err
	}
}
