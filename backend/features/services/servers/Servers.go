// Package servers announces which services each process runs (forge
// 21g-1), in KV svc_servers: <process> → Announcement, refreshed every
// Refresh and dropped by the bucket's TTL when a process stops
// refreshing, as connector adapters announce themselves.
package servers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("services")

const (
	// Bucket is the announcements' KV bucket.
	Bucket = "svc_servers"
	// Refresh is how often a process announces itself again.
	Refresh = 10 * time.Second
	// TTL is how long an announcement lasts unrefreshed.
	TTL = 30 * time.Second
)

// Announcement is what a process runs.
type Announcement struct {
	Process string `json:"process"`
	Host    string `json:"host"`
	Version string `json:"version"`
	// Set is the --services value it was started with.
	Set string `json:"set"`
	// Services are those it serves to other processes; Calls those it
	// reaches in others.
	Services  []string  `json:"services"`
	Calls     []string  `json:"calls"`
	Started   time.Time `json:"started"`
	Heartbeat time.Time `json:"heartbeat"`
	// Draining is set while it stops: it takes no new calls.
	Draining bool `json:"draining,omitempty"`
}

// Open is the bucket, made on first use.
func Open(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, natsd.KV(jetstream.KeyValueConfig{Bucket: Bucket, History: 1, TTL: TTL}))
	return kv, log.Wrap(err)
}

// Key is a process's key: its name with '.' and other subject characters
// as '_'.
func Key(process string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '*' || r == '>' || r == ' ' || r == '/' {
			return '_'
		}
		return r
	}, process)
}

// Announcer keeps one process's announcement current.
type Announcer struct {
	kv     jetstream.KeyValue
	a      func() Announcement
	cancel context.CancelFunc
	done   chan struct{}
}

// Announce writes a's announcement now and every Refresh until Stop; a is
// called each time, so the services it serves may still grow.
func Announce(ctx context.Context, js jetstream.JetStream, a func() Announcement) (*Announcer, error) {
	kv, err := Open(ctx, js)
	if err != nil {
		return nil, err
	}
	actx, cancel := context.WithCancel(context.Background())
	an := &Announcer{kv: kv, a: a, cancel: cancel, done: make(chan struct{})}
	if err := an.put(ctx, false); err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer close(an.done)
		t := time.NewTicker(Refresh)
		defer t.Stop()
		for {
			select {
			case <-actx.Done():
				return
			case <-t.C:
				if err := an.put(actx, false); err != nil && actx.Err() == nil {
					log.Warn(actx, "announcing this process: %v", err)
				}
			}
		}
	}()
	return an, nil
}

func (an *Announcer) put(ctx context.Context, draining bool) error {
	a := an.a()
	a.Heartbeat, a.Draining = time.Now().UTC(), draining
	data, err := json.Marshal(a)
	if err != nil {
		return log.Wrap(err)
	}
	_, err = an.kv.Put(ctx, Key(a.Process), data)
	return log.Wrap(err)
}

// Draining marks the process as draining, at once.
func (an *Announcer) Draining(ctx context.Context) {
	an.cancel()
	<-an.done
	if err := an.put(ctx, true); err != nil {
		log.Warn(ctx, "announcing the drain: %v", err)
	}
}

// Stop removes the announcement.
func (an *Announcer) Stop(ctx context.Context) {
	an.cancel()
	<-an.done
	_ = an.kv.Delete(ctx, Key(an.a().Process))
}

// List are the processes announced now, by name; an announcement past
// its TTL that the bucket has not dropped yet is left out.
func List(ctx context.Context, js jetstream.JetStream) ([]Announcement, error) {
	kv, err := Open(ctx, js)
	if err != nil {
		return nil, err
	}
	keys, err := kv.ListKeys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, log.Wrap(err)
	}
	var out []Announcement
	for k := range keys.Keys() {
		e, err := kv.Get(ctx, k)
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			continue
		}
		if err != nil {
			return nil, log.Wrap(err)
		}
		var a Announcement
		if json.Unmarshal(e.Value(), &a) != nil || time.Since(a.Heartbeat) > TTL {
			continue
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Announcement) int { return strings.Compare(a.Process, b.Process) })
	return out, nil
}
