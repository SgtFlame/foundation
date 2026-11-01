package impl

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// queueSize is how many frames may wait for a slow client before the
// connection is dropped; the client reconnects and resumes.
const queueSize = 4096

// Serve runs one live connection until the socket or args.Ctx ends.
func (s *service) Serve(args liveservice.ServeArgs) liveservice.ServeResponse {
	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	args.Ctx = ctx
	c := &conn{
		svc: s, id: strconv.FormatUint(features.NewTSID(), 10), args: args.ServiceArgs, cancel: cancel, socket: args.Socket,
		out: make(chan out, queueSize), commands: make(chan liveservice.CommandFrame, 256),
		subs: map[string]context.CancelFunc{}, calls: map[string]context.CancelFunc{}, presence: map[string]json.RawMessage{},
		binary: map[uint32]func([]byte){}, handlers: map[string]live.ConnHandler{},
	}
	go c.writeLoop(ctx)
	go c.commandLoop(ctx)
	go c.presenceLoop(ctx)
	c.attach()
	defer c.close()
	c.Send(liveservice.HelloFrame{Type: "hello", Connection: c.id, UserID: c.args.Principal.UserID, Color: presence.ColorOf(c.args.Principal.UserID)})

	for {
		isBinary, msg, err := args.Socket.Read(ctx)
		if err != nil {
			return liveservice.ServeResponse{Common: features.OK()}
		}
		if isBinary {
			c.handleBinary(msg)
			continue
		}
		c.handle(msg)
	}
}

type out struct {
	binary bool
	msg    []byte
}

type conn struct {
	svc    *service
	id     string
	args   features.ServiceArgs
	cancel context.CancelFunc
	socket liveservice.Socket
	out    chan out

	commands chan liveservice.CommandFrame

	mu       sync.Mutex
	subs     map[string]context.CancelFunc
	calls    map[string]context.CancelFunc
	presence map[string]json.RawMessage // objects this connection is in, with its state
	binary   map[uint32]func([]byte)
	handlers map[string]live.ConnHandler // by frame type
	attached []live.ConnHandler
}

// live.Conn, for the connection extensions

func (c *conn) ID() string                 { return c.id }
func (c *conn) Args() features.ServiceArgs { return c.args }

func (c *conn) Send(frame any) {
	msg, err := json.Marshal(frame)
	if err != nil {
		log.Error(c.args.Context(), err, "cannot encode a frame")
		return
	}
	c.queue(out{msg: msg})
}

func (c *conn) SendBinary(channel uint32, payload []byte) {
	msg := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(msg, channel)
	copy(msg[4:], payload)
	c.queue(out{binary: true, msg: msg})
}

func (c *conn) OnBinary(channel uint32, fn func([]byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.binary[channel] = fn
}

func (c *conn) CloseChannel(channel uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.binary, channel)
}

func (c *conn) queue(o out) {
	if c.args.Context().Err() != nil {
		return
	}
	select {
	case c.out <- o:
	default:
		log.Warn(c.args.Context(), "live connection %s is too slow; dropped", c.id)
		c.cancel()
	}
}

func (c *conn) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-c.out:
			if err := c.socket.Write(ctx, o.binary, o.msg); err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (c *conn) attach() {
	if c.svc.reg == nil {
		return
	}
	for _, ext := range live.Extensions.All(c.svc.reg) {
		h := ext.Attach(c)
		if h == nil {
			continue
		}
		c.attached = append(c.attached, h)
		for _, f := range ext.Frames {
			c.handlers[f] = h
		}
	}
}

func (c *conn) close() {
	c.mu.Lock()
	for _, cancel := range c.subs {
		cancel()
	}
	for _, cancel := range c.calls {
		cancel()
	}
	objects := c.presence
	c.presence = map[string]json.RawMessage{}
	c.mu.Unlock()
	for _, h := range c.attached {
		h.Close()
	}
	if c.svc.presence != nil {
		bg := context.WithoutCancel(c.args.Context())
		for object := range objects {
			c.svc.presence.Clear(bg, c.args.Principal.TenantID, object, c.id)
		}
	}
}

// traced is the connection's args with the frame's trace, so the work a
// frame causes logs together.
func (c *conn) traced(ctx context.Context, trace string) features.ServiceArgs {
	args := c.args
	if trace == "" {
		trace = logging.NewTraceID()
	}
	args.Ctx = logging.WithContext(ctx, logging.FromContext(ctx).Trace(trace))
	return args
}

func (c *conn) handleBinary(msg []byte) {
	if len(msg) < 4 {
		return
	}
	channel := binary.BigEndian.Uint32(msg)
	c.mu.Lock()
	fn := c.binary[channel]
	c.mu.Unlock()
	if fn != nil {
		fn(msg[4:])
	}
}

func (c *conn) handle(msg []byte) {
	var head liveservice.Head
	if err := json.Unmarshal(msg, &head); err != nil {
		return
	}
	switch head.Type {
	case "ping":
		c.Send(liveservice.Head{Type: "pong"})
	case "command":
		var f liveservice.CommandFrame
		if json.Unmarshal(msg, &f) == nil {
			select {
			case c.commands <- f:
			default:
				c.Send(liveservice.RejectedFrame{Type: "rejected", ID: f.ID, Code: http.StatusTooManyRequests, Message: "too many commands at once", ErrorCode: "busy"})
			}
		}
	case "subscribe":
		var f liveservice.SubscribeFrame
		if json.Unmarshal(msg, &f) == nil {
			c.subscribe(f)
		}
	case "unsubscribe":
		var f liveservice.UnsubscribeFrame
		if json.Unmarshal(msg, &f) == nil {
			c.unsubscribe(f.Sub)
		}
	case "presence":
		var f liveservice.PresenceFrame
		if json.Unmarshal(msg, &f) == nil {
			c.setPresence(f)
		}
	case "call":
		var f liveservice.CallFrame
		if json.Unmarshal(msg, &f) == nil {
			c.call(f)
		}
	case "cancel":
		var f liveservice.CancelFrame
		if json.Unmarshal(msg, &f) == nil {
			c.mu.Lock()
			if cancel := c.calls[f.ID]; cancel != nil {
				cancel()
			}
			c.mu.Unlock()
		}
	default:
		if h := c.handlers[head.Type]; h != nil {
			h.Handle(head.Type, msg)
		}
	}
}

// commandLoop decides the connection's commands one at a time, in the
// order they were sent.
func (c *conn) commandLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-c.commands:
			c.Send(c.execute(ctx, f))
		}
	}
}

func (c *conn) execute(ctx context.Context, f liveservice.CommandFrame) any {
	if c.svc.bus == nil {
		return liveservice.RejectedFrame{Type: "rejected", ID: f.ID, Code: http.StatusServiceUnavailable, Message: "commands are not available"}
	}
	res := c.svc.bus.Execute(bus.Request{ServiceArgs: c.traced(ctx, f.Trace), ID: f.ID, Type: f.Command, Target: f.Target, RequestID: f.Request, Args: f.Args})
	if res.Common.Code >= 300 {
		return liveservice.RejectedFrame{Type: "rejected", ID: f.ID, Code: res.Common.Code, Message: res.Common.Message, ErrorCode: res.Common.ErrorCode, Problems: res.Problems}
	}
	return liveservice.AckFrame{Type: "ack", ID: f.ID, Stream: res.Stream, Seq: res.Seq, Duplicate: res.Duplicate}
}

type subSink struct {
	c   *conn
	sub string
}

func (s subSink) Snapshot(data any, pos live.Position) {
	s.c.Send(liveservice.SnapshotFrame{Type: "snapshot", Sub: s.sub, Pos: pos, Data: data})
}
func (s subSink) Change(delta live.Delta, pos live.Position) {
	s.c.Send(liveservice.ChangeFrame{Type: "change", Sub: s.sub, Pos: pos, Delta: delta})
}
func (s subSink) Failed(resp features.ServiceResponse) {
	s.c.unsubscribe(s.sub)
	s.c.Send(liveservice.SubErrorFrame{Type: "subError", Sub: s.sub, Code: resp.Code, Message: resp.Message, ErrorCode: resp.ErrorCode})
}

func (c *conn) subscribe(f liveservice.SubscribeFrame) {
	fail := func(code uint16, message, errorCode string) {
		c.Send(liveservice.SubErrorFrame{Type: "subError", Sub: f.Sub, Code: code, Message: message, ErrorCode: errorCode})
	}
	if c.svc.subs == nil {
		fail(http.StatusServiceUnavailable, "queries are not available", "")
		return
	}
	c.mu.Lock()
	if old := c.subs[f.Sub]; old != nil {
		old() // a resubscribe replaces the subscription
	}
	if len(c.subs) >= c.svc.maxSubscriptions() {
		c.mu.Unlock()
		fail(http.StatusTooManyRequests, "this connection holds too many subscriptions", "too_many_subscriptions")
		return
	}
	ctx, cancel := context.WithCancel(c.args.Context())
	c.subs[f.Sub] = cancel
	c.mu.Unlock()
	args := c.traced(ctx, f.Trace)
	if resp := c.svc.subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: f.Query, Params: f.Params, After: f.After, Sink: subSink{c, f.Sub}}); resp != nil {
		c.unsubscribe(f.Sub)
		fail(resp.Code, resp.Message, resp.ErrorCode)
	}
}

func (c *conn) unsubscribe(sub string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.subs[sub]; cancel != nil {
		cancel()
		delete(c.subs, sub)
	}
}

func (c *conn) setPresence(f liveservice.PresenceFrame) {
	if c.svc.presence == nil || !presence.ValidObject(f.Object) {
		return
	}
	ctx := c.args.Context()
	if f.Leave {
		c.mu.Lock()
		delete(c.presence, f.Object)
		c.mu.Unlock()
		c.svc.presence.Clear(ctx, c.args.Principal.TenantID, f.Object, c.id)
		return
	}
	c.mu.Lock()
	c.presence[f.Object] = f.State
	c.mu.Unlock()
	if err := c.svc.presence.Set(ctx, c.args.Principal, f.Object, c.id, f.State); err != nil {
		log.Error(ctx, err, "cannot set presence in %s", f.Object)
	}
}

// presenceLoop refreshes the connection's presence before it expires.
func (c *conn) presenceLoop(ctx context.Context) {
	if c.svc.presence == nil {
		return
	}
	tick := time.NewTicker(presence.Refresh)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c.mu.Lock()
			objects := make(map[string]json.RawMessage, len(c.presence))
			for k, v := range c.presence {
				objects[k] = v
			}
			c.mu.Unlock()
			for object, state := range objects {
				_ = c.svc.presence.Set(ctx, c.args.Principal, object, c.id, state)
			}
		}
	}
}

func (c *conn) call(f liveservice.CallFrame) {
	end := func(code uint16, message, errorCode string) {
		c.Send(liveservice.EndFrame{Type: "end", ID: f.ID, Code: code, Message: message, ErrorCode: errorCode})
	}
	var call *live.Call
	if c.svc.reg != nil {
		for _, k := range live.Calls.All(c.svc.reg) {
			if k.ID == f.Call {
				call = &k
				break
			}
		}
	}
	if call == nil {
		end(http.StatusNotFound, "there is no call "+f.Call, "unknown_call")
		return
	}
	c.mu.Lock()
	if len(c.calls) >= c.svc.maxCalls {
		c.mu.Unlock()
		end(http.StatusTooManyRequests, "too many calls at once", "busy")
		return
	}
	ctx, cancel := context.WithCancel(c.args.Context())
	c.calls[f.ID] = cancel
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.calls, f.ID)
			c.mu.Unlock()
			cancel()
		}()
		resp := call.Run(live.CallRun{ServiceArgs: c.traced(ctx, f.Trace), Args: f.Args, Emit: func(event string, data any) {
			c.Send(liveservice.EventFrame{Type: "event", ID: f.ID, Event: event, Data: data})
		}})
		if resp != nil && resp.Code >= 300 {
			end(resp.Code, resp.Message, resp.ErrorCode)
			return
		}
		end(http.StatusOK, "", "")
	}()
}
