package events

import (
	"context"

	"encoding/json"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/nats-io/nats.go"
)

// Prefix is where buses travel between processes: bus.<name>.
const Prefix = "bus"

// Subject is a bus's subject: bus.<name>.
func Subject(name string) string { return Prefix + "." + name }

// Headers on a bus's messages.
const (
	// HeaderOrigin names the bus that published the event, so it is not
	// delivered twice where it was published.
	HeaderOrigin = "Bus-Origin"
	// HeaderTenant is the event's tenant when it has one (Tenanted).
	HeaderTenant = "Bus-Tenant"
)

// Tenanted is an event that knows its tenant, sent in the HeaderTenant
// header for the subjects' readers (logs, permissions later).
type Tenanted interface{ BusTenant() uint64 }

// Over links b to the NATS subject bus.<name> (forge 21f-2): every event
// published here is also published there as JSON, and every event another
// process publishes there is delivered here. Within this process delivery
// stays synchronous; across processes it is asynchronous and at most once,
// so a subscriber that must not miss an event follows a durable bus
// (events/durable) instead. stop unlinks and unsubscribes.
func Over[T any](b *Bus[T], nc *nats.Conn, name string) (stop func(), err error) {
	origin := strconv.FormatUint(features.NewTSID(), 10)
	subject := Subject(name)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		if m.Header.Get(HeaderOrigin) == origin {
			return
		}
		var e T
		if json.Unmarshal(m.Data, &e) != nil {
			log.Warn(context.Background(), "bus %s: an event that cannot be read was dropped", name)
			return
		}
		b.Deliver(e)
	})
	if err == nil {
		err = nc.Flush() // subscribed at the server before Over returns
	}
	if err != nil {
		return nil, err
	}
	unlink := b.Link(func(e T) bool {
		data, err := json.Marshal(e)
		if err != nil {
			log.Warn(context.Background(), "bus %s: an event that cannot be sent stays in this process: %v", name, err)
			return true
		}
		m := nats.NewMsg(subject)
		m.Data = data
		m.Header.Set(HeaderOrigin, origin)
		if t, ok := any(e).(Tenanted); ok && t.BusTenant() != 0 {
			m.Header.Set(HeaderTenant, strconv.FormatUint(t.BusTenant(), 10))
		}
		if err := nc.PublishMsg(m); err != nil {
			log.Warn(context.Background(), "bus %s: not sent to other processes: %v", name, err)
		}
		return true
	})
	return func() {
		unlink()
		_ = sub.Unsubscribe()
	}, nil
}

var log = logging.In("events")
