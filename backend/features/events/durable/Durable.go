// Package durable carries a bus's events between processes through a
// JetStream stream (forge 21f-2), for a follower that must not miss one,
// such as the index following volume files.
package durable

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/events"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("events")

// Retention is how long a durable bus keeps an event no follower took.
const Retention = 24 * time.Hour

// StreamName is a durable bus's stream: BUS_<name>, '.' as '_'.
func StreamName(name string) string { return "BUS_" + strings.ReplaceAll(name, ".", "_") }

// Link makes b durable: every event published here goes to the stream
// capturing bus.<name> and is not delivered here at once. When follow,
// the durable consumer named consumer (shared by every process following
// it, so each event is delivered once) delivers the events here in order,
// each acked after its subscribers ran, and resumes where it stopped
// after a restart. A process that only publishes passes follow false. An
// event the stream refuses is delivered here, so it is never lost
// silently. stop unlinks and stops following.
func Link[T any](ctx context.Context, js jetstream.JetStream, b *events.Bus[T], name, consumer string, follow bool) (stop func(), err error) {
	subject := events.Subject(name)
	stream, err := js.CreateOrUpdateStream(ctx, natsd.Stream(jetstream.StreamConfig{
		Name:     StreamName(name),
		Subjects: []string{subject},
		MaxAge:   Retention,
		Storage:  jetstream.FileStorage,
	}))
	if err != nil {
		return nil, log.Wrap(err)
	}
	var cc jetstream.ConsumeContext
	if follow {
		c, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       consumer,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			MaxAckPending: 1, // in order
		})
		if err != nil {
			return nil, log.Wrap(err)
		}
		cc, err = c.Consume(func(m jetstream.Msg) {
			var e T
			if json.Unmarshal(m.Data(), &e) != nil {
				log.Warn(context.Background(), "bus %s: an event that cannot be read was dropped", name)
				_ = m.Term()
				return
			}
			b.Deliver(e)
			_ = m.Ack()
		})
		if err != nil {
			return nil, log.Wrap(err)
		}
	}
	unlink := b.Link(func(e T) bool {
		data, err := json.Marshal(e)
		if err == nil {
			pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = js.Publish(pctx, subject, data)
			cancel()
		}
		if err != nil {
			log.Warn(context.Background(), "bus %s: delivered in this process only: %v", name, err)
			return true
		}
		return false
	})
	return func() {
		unlink()
		if cc != nil {
			cc.Stop()
		}
	}, nil
}
