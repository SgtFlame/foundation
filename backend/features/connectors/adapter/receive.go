package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go"
)

// Received is the reply to a callback: how many events it held, each
// already in CX_<tenant> (26b-1).
type Received struct {
	Events int `json:"events"`
}

// ReceiveTimeout bounds verifying a callback and publishing its events.
const ReceiveTimeout = 10 * time.Second

// receiveSubjects are what the adapter answers callbacks on: every type
// with a Receive.
func (s *server) receiveSubjects() map[string][]string {
	out := map[string][]string{}
	for id, im := range s.impls {
		if im.Receive == nil {
			continue
		}
		if len(s.o.Tenants) == 0 {
			out[id] = []string{connectors.ReceiveSubjects(id)}
			continue
		}
		for _, t := range s.o.Tenants {
			out[id] = append(out[id], connectors.TenantReceiveSubjects(t, id))
		}
	}
	return out
}

// receiveCallback takes a callback from the bus, beside the calls.
func (s *server) receiveCallback(m *nats.Msg) {
	s.wg.Add(1)
	s.sem <- struct{}{}
	go func() {
		defer func() { <-s.sem; s.wg.Done() }()
		s.handleCallback(m)
	}()
}

// handleCallback verifies a callback with its connection's settings (the
// type's Receive), publishes the events it gives, and only then replies:
// a refusal is invalid (the hook answers 400), a failed publish
// unavailable (503, so the service sends it again; the events' ids keep a
// second delivery from counting twice).
func (s *server) handleCallback(m *nats.Msg) {
	tenant, typ, connection, err := connectors.ParseReceiveSubject(m.Subject)
	if err != nil {
		s.reply(m, nil, connectors.Errorf(connectors.CodeInvalid, "%v", err))
		return
	}
	im, ok := s.impls[typ]
	if !ok || im.Receive == nil {
		s.reply(m, nil, connectors.Errorf(connectors.CodeNotSupported, "this adapter receives no callbacks for %s", typ))
		return
	}
	var cb Callback
	if err := json.Unmarshal(m.Data, &cb); err != nil {
		s.reply(m, nil, connectors.Errorf(connectors.CodeInvalid, "the callback is not readable"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ReceiveTimeout)
	defer cancel()
	b := log.With("type", typ, "connection", connection)
	ctx = logging.WithContext(ctx, b)
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
	x := Context{Context: ctx, Args: args, Connection: connection, Log: b, books: s.books}
	events, e := s.verify(x, im, tenant, typ, connection, cb)
	if e != nil {
		b.Debug(ctx, "a callback for %s was refused: %s", typ, e.Code) // never its body
		s.reply(m, nil, e)
		return
	}
	for _, ev := range events {
		if !slices.Contains(s.types[typ].Contracts, ev.Contract) {
			s.reply(m, nil, connectors.Errorf(connectors.CodeInvalid, "%s does not implement %s", typ, ev.Contract))
			return
		}
		at := ev.At
		if at.IsZero() {
			at = cb.ReceivedAt
		}
		if err := s.books.Publish(ctx, tenant, connection, ev.Contract, ev.Name, ev.ID, ev.Payload, at); err != nil {
			b.Warn(ctx, "a callback's event for %s could not be published: %v", typ, err)
			s.reply(m, nil, connectors.Errorf(connectors.CodeUnavailable, "the event could not be kept; send it again"))
			return
		}
	}
	raw, _ := json.Marshal(Received{Events: len(events)})
	s.reply(m, raw, nil)
}

// verify runs the type's Receive on the connection's settings.
func (s *server) verify(x Context, im Impl, tenant uint64, typ, connection string, cb Callback) (events []Event, e *connectors.Error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error(x, fmt.Errorf("%v", r), "%s's Receive panicked", typ)
			events, e = nil, connectors.Errorf(connectors.CodeUnavailable, "%s failed", typ)
		}
	}()
	conn, err := s.resolve(x, tenant, typ, connection)
	if err != nil {
		return nil, connectors.AsError(err)
	}
	events, err = im.Receive.Receive(x, conn, cb)
	if err != nil {
		if ce, ok := err.(*connectors.Error); ok {
			return nil, ce
		}
		return nil, connectors.Errorf(connectors.CodeInvalid, "%v", err)
	}
	return events, nil
}
