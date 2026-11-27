package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
)

// The live core for Go callers in other processes (forge 21d-4).

func requestOf(args features.ServiceArgs, c liveservice.Command) bus.Request {
	return bus.Request{ServiceArgs: args, ID: c.ID, Type: c.Type, Target: c.Target, RequestID: c.RequestID,
		App: c.App, Actor: c.Actor, Undoes: c.Undoes, Branch: c.Branch, Args: c.Args}
}

func committedOf(r bus.Result) liveservice.Committed {
	return liveservice.Committed{Stream: r.Stream, Target: r.Target, Seq: r.Seq, Duplicate: r.Duplicate, RequestID: r.RequestID, Problems: r.Problems}
}

func (s *service) noCore(args features.ServiceArgs) *features.ServiceResponse {
	return args.Fail(http.StatusServiceUnavailable, log.Code("live_core_unavailable").New("the live core is not served here"))
}

func (s *service) Execute(args liveservice.ExecuteArgs) liveservice.ExecuteResponse {
	if s.bus == nil {
		return liveservice.ExecuteResponse{Common: *s.noCore(args.ServiceArgs)}
	}
	r := s.bus.Execute(requestOf(args.ServiceArgs, args.Command))
	return liveservice.ExecuteResponse{Common: r.Common, Committed: committedOf(r)}
}

func (s *service) Transact(args liveservice.TransactArgs) liveservice.TransactResponse {
	if s.bus == nil {
		return liveservice.TransactResponse{Common: *s.noCore(args.ServiceArgs)}
	}
	r := s.bus.Transact(requestOf(args.ServiceArgs, args.Command), args.Ops, args.DryRun)
	out := liveservice.TransactResponse{Common: r.Common, Committed: committedOf(r.Result)}
	for _, d := range r.Ops {
		out.Ops = append(out.Ops, liveservice.DecidedOp{Op: d.Op, Events: d.Events})
	}
	return out
}

func (s *service) Read(args liveservice.ReadArgs) liveservice.ReadResponse {
	if args.Principal.Kind != features.PrincipalService {
		return liveservice.ReadResponse{Common: *args.Fail(http.StatusForbidden, log.Code("services_only").New("only services may read projections this way"))}
	}
	if s.store == nil {
		return liveservice.ReadResponse{Common: *s.noCore(args.ServiceArgs)}
	}
	v, ok, err := s.store.Get(args.Context(), args.Principal.TenantID, args.Projector, args.Key)
	if err != nil {
		return liveservice.ReadResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return liveservice.ReadResponse{Common: features.OK(), Found: ok, Value: v}
}

// Subscribe opens the query and delivers its results; a subscriber that
// falls behind holds the query back rather than losing a change, since a
// lost change would leave its copy wrong.
func (s *service) Subscribe(args liveservice.SubscribeArgs) *liveservice.SubscribeRequest {
	events := make(chan liveservice.SubscribeEvent, 16)
	if s.subs == nil {
		f := s.noCore(args.ServiceArgs)
		events <- liveservice.SubscribeEvent{Failed: f}
		close(events)
		return &liveservice.SubscribeRequest{Events: events}
	}
	sink := &coreSink{ctx: args.Context(), events: events}
	if f := s.subs.Open(subscribeArgs(args, sink)); f != nil {
		events <- liveservice.SubscribeEvent{Failed: f}
		close(events)
		return &liveservice.SubscribeRequest{Events: events}
	}
	go func() {
		<-args.Context().Done()
		sink.close()
	}()
	return &liveservice.SubscribeRequest{Events: events}
}

func marshal(v any) json.RawMessage {
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func subscribeArgs(args liveservice.SubscribeArgs, sink *coreSink) subscribe.OpenArgs {
	params := args.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}
	return subscribe.OpenArgs{ServiceArgs: args.ServiceArgs, Query: args.Query, Params: params, Sink: sink}
}

// coreSink sends a subscription's results to its channel until the
// subscription's context ends.
type coreSink struct {
	ctx    context.Context
	mu     sync.Mutex
	done   bool
	events chan liveservice.SubscribeEvent
}

func (k *coreSink) send(e liveservice.SubscribeEvent) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.done {
		return
	}
	select {
	case k.events <- e:
	case <-k.ctx.Done():
	}
}

func (k *coreSink) close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.done {
		k.done = true
		close(k.events)
	}
}

func (k *coreSink) Snapshot(data any, pos live.Position) {
	k.send(liveservice.SubscribeEvent{Snapshot: true, Data: marshal(data), Position: pos})
}

func (k *coreSink) Change(delta live.Delta, pos live.Position) {
	k.send(liveservice.SubscribeEvent{Delta: &delta, Position: pos})
}

func (k *coreSink) Failed(resp features.ServiceResponse) {
	k.send(liveservice.SubscribeEvent{Failed: &resp})
}
