// Package subscribe opens queries as subscriptions (documentation/Live.md ›
// Queries are subscriptions): it checks access, runs the query's feed and
// hands its snapshot and changes to a sink until the subscription closes.
package subscribe

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

var log = live.Log.Tags("subscribe")

// Sink receives one subscription's results, in order.
type Sink interface {
	Snapshot(data any, pos live.Position)
	Change(delta live.Delta, pos live.Position)
	// Failed ends the subscription with a failure.
	Failed(resp features.ServiceResponse)
}

// Subscriptions opens queries over a store.
type Subscriptions struct {
	store live.Store
	reg   *extensions.Registry
	// Resolve finds a query no feature registered, such as one a tenant's
	// published query graph defines (live/nodes).
	Resolve func(args features.ServiceArgs, typ string) (live.Query, bool)
}

func New(store live.Store, reg *extensions.Registry) *Subscriptions {
	return &Subscriptions{store: store, reg: reg}
}

// Query finds a query type.
func (s *Subscriptions) Query(typ string) (live.Query, bool) {
	for _, q := range live.Queries.All(s.reg) {
		if q.ID == typ {
			return q, true
		}
	}
	return live.Query{}, false
}

// OpenArgs opens one subscription.
type OpenArgs struct {
	features.ServiceArgs // its context ends the subscription
	Query                string
	Params               json.RawMessage
	After                live.Position // where the client resumes; it gets a fresh snapshot
	Sink                 Sink
}

// Open checks access and starts the query's feed; it returns at once, with
// a failure when the subscription could not open. The feed runs until
// args.Ctx ends.
func (s *Subscriptions) Open(args OpenArgs) *features.ServiceResponse {
	q, ok := s.Query(args.Query)
	if !ok && s.Resolve != nil {
		q, ok = s.Resolve(args.ServiceArgs, args.Query)
	}
	if !ok {
		return args.Fail(http.StatusNotFound, log.Code("unknown_query").Errorf("there is no query %q", args.Query))
	}
	resource := authz.Tenant(args.Principal)
	if q.Resource != nil {
		resource = q.Resource(args.Principal, args.Params)
	}
	if q.Action != "" {
		if err := authz.Check(args.Context(), args.Principal, q.Action, resource).Err(q.Action, resource); err != nil {
			return args.Fail(authz.Status, err)
		}
	}
	if err := tools.CheckArgs(q.Params, args.Params); err != nil {
		return args.Fail(http.StatusBadRequest, log.Code("invalid_params").New(err.Error()))
	}
	f := &feed{args: args, store: s.store}
	go func() {
		if err := q.Run(f); err != nil && args.Context().Err() == nil {
			args.Sink.Failed(*args.Fail(http.StatusInternalServerError, log.Wrapf(err, "query %s failed", args.Query)))
		}
	}()
	return nil
}

// Once is a one-time read: the query's snapshot, after which the
// subscription closes.
func (s *Subscriptions) Once(args features.ServiceArgs, query string, params json.RawMessage) (any, live.Position, *features.ServiceResponse) {
	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	args.Ctx = ctx
	got := make(chan once, 1)
	if f := s.Open(OpenArgs{ServiceArgs: args, Query: query, Params: params, Sink: &onceSink{got: got}}); f != nil {
		return nil, nil, f
	}
	select {
	case r := <-got:
		return r.data, r.pos, r.fail
	case <-ctx.Done():
		return nil, nil, args.Fail(http.StatusRequestTimeout, log.Code("cancelled").New("the read was cancelled"))
	}
}

type once struct {
	data any
	pos  live.Position
	fail *features.ServiceResponse
}

type onceSink struct{ got chan once }

func (o *onceSink) Snapshot(data any, pos live.Position) {
	select {
	case o.got <- once{data: data, pos: pos}:
	default:
	}
}
func (o *onceSink) Change(live.Delta, live.Position) {}
func (o *onceSink) Failed(resp features.ServiceResponse) {
	select {
	case o.got <- once{fail: &resp}:
	default:
	}
}

type feed struct {
	args  OpenArgs
	store live.Store
}

func (f *feed) Args() features.ServiceArgs { return f.args.ServiceArgs }
func (f *feed) Context() context.Context   { return f.args.Context() }
func (f *feed) Params() json.RawMessage    { return f.args.Params }
func (f *feed) Store() live.Store          { return f.store }
func (f *feed) Snapshot(data any, pos live.Position) {
	if f.Context().Err() == nil {
		f.args.Sink.Snapshot(data, pos)
	}
}
func (f *feed) Change(delta live.Delta, pos live.Position) {
	if f.Context().Err() == nil {
		f.args.Sink.Change(delta, pos)
	}
}
