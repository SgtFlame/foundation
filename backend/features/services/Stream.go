package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/nats-io/nats.go"
)

// Streams (forge 21c): an operation that hands back an <Op>Request holding
// channels travels as a stream on the call's reply inbox. The server sends
// a head (the request's other fields, such as logsservice.TailRequest's
// Common), then each value of each channel as {f, seq, v}, a channel's end
// as {f, closed}, heartbeats while idle, and {done} last. A channel whose
// values carry a Common (a response channel, such as chatservice's
// Response) ends after its first value; any other ends when the
// implementation closes it.
const (
	// Window is how many values a server sends before the caller acks.
	Window = 64
	// Heartbeat is how often an idle stream says it is alive.
	Heartbeat = 5 * time.Second
	// StreamIdle is how long a caller waits for anything before the
	// stream is lost (503 service_lost).
	StreamIdle = 3 * Heartbeat
	// CodeLost is 503: the server of a stream went away.
	CodeLost = "service_lost"
	// HeaderAck names the subject a caller acks a stream's values on.
	HeaderAck = "Svc-Ack"
)

// streamIdle is StreamIdle, shortened by tests.
var streamIdle = StreamIdle

// frame is one message of a stream.
type frame struct {
	Head   json.RawMessage `json:"head,omitempty"`
	Field  string          `json:"f,omitempty"`
	Seq    uint64          `json:"seq,omitempty"`
	Value  json.RawMessage `json:"v,omitempty"`
	Closed bool            `json:"closed,omitempty"`
	HB     bool            `json:"hb,omitempty"`
	Done   bool            `json:"done,omitempty"`
	// Failed ends a stream that could not start (the head is a failure).
	Failed *features.ServiceResponse `json:"failed,omitempty"`
}

// Streamed is the Handler of a streamed operation fn.
func Streamed[A, R any](fn func(A) R) Handler {
	mustStruct[A]()
	return func(a features.ServiceArgs, body []byte) (any, error) {
		var args A
		if err := Decode(body, &args); err != nil {
			return nil, err
		}
		setArgs(&args, a)
		return streamOf{fn(args)}, nil
	}
}

type streamOf struct{ req any }

// chanFields are the exported channel fields of a request struct type.
func chanFields(t reflect.Type) []int {
	var out []int
	for i := 0; i < t.NumField(); i++ {
		if f := t.Field(i); f.IsExported() && f.Type.Kind() == reflect.Chan {
			out = append(out, i)
		}
	}
	return out
}

// isResponseChan reports whether a channel's values carry a Common.
func isResponseChan(t reflect.Type) bool {
	e := t.Elem()
	if e.Kind() == reflect.Pointer {
		e = e.Elem()
	}
	_, ok := commonField(reflect.New(e).Elem())
	return ok
}

// head is the request's fields that are not channels, as JSON.
func head(req reflect.Value) (json.RawMessage, error) {
	m := map[string]any{}
	t := req.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Type.Kind() == reflect.Chan {
			continue
		}
		m[f.Name] = req.Field(i).Interface()
	}
	return json.Marshal(m)
}

// serveStream sends req's head and values to m's reply inbox until every
// channel ends or ctx does.
func (s *Server) serveStream(ctx context.Context, m *nats.Msg, op string, req any) {
	rv := reflect.ValueOf(req)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		s.sendFrame(m.Reply, frame{Failed: ptr(Failed(http.StatusInternalServerError, "service_bad_stream", ""))})
		return
	}
	rv = rv.Elem()
	h, err := head(rv)
	if err != nil {
		s.sendFrame(m.Reply, frame{Failed: ptr(Failed(http.StatusInternalServerError, "service_bad_stream", ""))})
		return
	}
	// Acks come back on a subject of the server's own.
	credits := make(chan int, Window)
	ackSub, err := s.nc.Subscribe(nats.NewInbox(), func(a *nats.Msg) {
		n, _ := strconv.Atoi(string(a.Data))
		select {
		case credits <- n:
		default:
		}
	})
	if err != nil {
		s.sendFrame(m.Reply, frame{Failed: ptr(Failed(http.StatusInternalServerError, "service_bad_stream", ""))})
		return
	}
	defer ackSub.Unsubscribe()
	hm := nats.NewMsg(m.Reply)
	hm.Header.Set(HeaderAck, ackSub.Subject)
	hb, _ := json.Marshal(frame{Head: h})
	hm.Data = hb
	if err := s.nc.PublishMsg(hm); err != nil {
		return
	}

	type value struct {
		field  string
		v      reflect.Value
		closed bool
	}
	values := make(chan value)
	var wg sync.WaitGroup
	t := rv.Type()
	for _, i := range chanFields(t) {
		ch := rv.Field(i)
		if ch.IsNil() {
			continue
		}
		name, once := t.Field(i).Name, isResponseChan(t.Field(i).Type)
		wg.Add(1)
		go func() {
			defer wg.Done()
			cases := []reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: ch}, {Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}}
			for {
				chosen, v, ok := reflect.Select(cases)
				if chosen == 1 {
					return
				}
				if !ok {
					select {
					case values <- value{field: name, closed: true}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case values <- value{field: name, v: v}:
				case <-ctx.Done():
					return
				}
				if once {
					select {
					case values <- value{field: name, closed: true}:
					case <-ctx.Done():
					}
					return
				}
			}
		}()
	}
	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()

	var seq uint64
	window := Window
	tick := time.NewTicker(Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.sendFrame(m.Reply, frame{Done: true})
			return
		case <-allDone:
			s.sendFrame(m.Reply, frame{Done: true})
			return
		case <-tick.C:
			s.sendFrame(m.Reply, frame{HB: true})
		case v := <-values:
			if v.closed {
				s.sendFrame(m.Reply, frame{Field: v.field, Closed: true})
				continue
			}
			for window <= 0 {
				select {
				case n := <-credits:
					window += n
				case <-ctx.Done():
					s.sendFrame(m.Reply, frame{Done: true})
					return
				case <-tick.C:
					s.sendFrame(m.Reply, frame{HB: true})
				}
			}
			body, err := Encode(v.v.Interface())
			if err != nil {
				log.Error(ctx, err, "svc.stream %s.%s: a value cannot be encoded", s.service, op)
				continue
			}
			seq++
			window--
			s.sendFrame(m.Reply, frame{Field: v.field, Seq: seq, Value: body})
		}
	}
}

func (s *Server) sendFrame(subject string, f frame) {
	b, _ := json.Marshal(f)
	_ = s.nc.Publish(subject, b)
}

func ptr[T any](v T) *T { return &v }

// Stream sends a streamed op of service with args and returns its request
// R (a pointer to an <Op>Request) once the head arrives, its channels fed
// from the stream. Failures to start come back through the request: its
// Common when it has one, else its response channels, its other channels
// closed.
func Stream[R any](c *Client, service, op string, args any, opts ...CallOption) R {
	var r R
	rt := reflect.TypeOf(r)
	if rt.Kind() != reflect.Pointer || rt.Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("services: %T is not a pointer to a request", r))
	}
	a := ArgsOf(args)
	ctx := a.Context()
	fail := func(sr features.ServiceResponse, err error) R {
		log.With("service", service, "op", op, "code", sr.Code).Warn(ctx, "svc.stream %s.%s failed: %v", service, op, err)
		req := reflect.New(rt.Elem())
		if f, ok := commonField(req.Elem()); ok {
			f.Set(reflect.ValueOf(sr))
		}
		fillChannels(req.Elem(), sr)
		reflect.ValueOf(&r).Elem().Set(req)
		return r
	}
	msg, err := c.request(ctx, service, op, args, opts, true)
	if err != nil {
		var sr *callError
		if errors.As(err, &sr) {
			return fail(sr.resp, sr.err)
		}
		return fail(Failed(http.StatusBadRequest, CodeBadRequest, ""), err)
	}
	inbox := nats.NewInbox()
	sub, err := c.NC.SubscribeSync(inbox)
	if err != nil {
		return fail(Unavailable(service), err)
	}
	msg.Reply = inbox
	callID := msg.Header.Get(HeaderCall)
	if err := c.NC.PublishMsg(msg); err != nil {
		_ = sub.Unsubscribe()
		return fail(Unavailable(service), err)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// No responders arrives as a status message on the inbox.
	first, err := sub.NextMsg(timeout)
	if err != nil || first.Header.Get("Status") == "503" {
		_ = sub.Unsubscribe()
		if err == nil || errors.Is(err, nats.ErrNoResponders) {
			return fail(Unavailable(service), errors.New("no responders"))
		}
		return fail(Timeout(service), err)
	}
	var hf frame
	if err := json.Unmarshal(first.Data, &hf); err != nil {
		_ = sub.Unsubscribe()
		return fail(Failed(http.StatusBadGateway, CodeBadReply, ""), err)
	}
	if hf.Head == nil {
		_ = sub.Unsubscribe()
		var plain failure
		if hf.Failed != nil {
			return fail(*hf.Failed, errors.New(hf.Failed.Message))
		}
		if json.Unmarshal(first.Data, &plain) == nil && plain.Common.Code != 0 {
			return fail(plain.Common, errors.New(plain.Common.Message))
		}
		return fail(Failed(http.StatusBadGateway, CodeBadReply, ""), errors.New("no head"))
	}
	req := reflect.New(rt.Elem())
	if err := json.Unmarshal(hf.Head, req.Interface()); err != nil {
		_ = sub.Unsubscribe()
		return fail(Failed(http.StatusBadGateway, CodeBadReply, ""), err)
	}
	chans := map[string]reflect.Value{}
	once := map[string]bool{}
	t := rt.Elem()
	for _, i := range chanFields(t) {
		ft := t.Field(i).Type
		ch := reflect.MakeChan(reflect.ChanOf(reflect.BothDir, ft.Elem()), Window)
		req.Elem().Field(i).Set(ch.Convert(ft))
		chans[t.Field(i).Name] = ch
		once[t.Field(i).Name] = isResponseChan(ft)
	}
	reflect.ValueOf(&r).Elem().Set(req)
	ack := first.Header.Get(HeaderAck)

	go func() {
		defer sub.Unsubscribe()
		closed := map[string]bool{}
		end := func(lost bool) {
			for name, ch := range chans {
				if closed[name] {
					continue
				}
				if lost && once[name] {
					v := reflect.New(ch.Type().Elem())
					target := v.Elem()
					if target.Kind() == reflect.Pointer {
						target.Set(reflect.New(target.Type().Elem()))
						target = target.Elem()
					}
					if f, ok := commonField(target); ok {
						f.Set(reflect.ValueOf(Failed(http.StatusServiceUnavailable, CodeLost, "The "+service+" service went away.")))
						ch.TrySend(v.Elem())
					}
				}
				ch.Close()
				closed[name] = true
			}
		}
		received := 0
		for {
			nctx, cancel := context.WithTimeout(ctx, streamIdle)
			m, err := sub.NextMsgWithContext(nctx)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					// The caller went away: cancel the server's call.
					_ = c.NC.Publish(CancelSubject(service, callID), nil)
					end(false)
					return
				}
				end(true)
				return
			}
			var f frame
			if json.Unmarshal(m.Data, &f) != nil {
				continue
			}
			switch {
			case f.Done:
				end(false)
				return
			case f.HB:
			case f.Closed:
				if ch, ok := chans[f.Field]; ok && !closed[f.Field] {
					ch.Close()
					closed[f.Field] = true
				}
			case f.Field != "":
				ch, ok := chans[f.Field]
				if !ok || closed[f.Field] {
					continue
				}
				v := reflect.New(ch.Type().Elem())
				if err := json.Unmarshal(f.Value, v.Interface()); err != nil {
					continue
				}
				// A caller that stops reading holds the stream back; one
				// that goes away ends it.
				sent, _, _ := reflect.Select([]reflect.SelectCase{
					{Dir: reflect.SelectSend, Chan: ch, Send: v.Elem()},
					{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())},
				})
				if sent == 1 {
					_ = c.NC.Publish(CancelSubject(service, callID), nil)
					end(false)
					return
				}
				received++
				if ack != "" && received >= Window/2 {
					_ = c.NC.Publish(ack, []byte(strconv.Itoa(received)))
					received = 0
				}
			}
		}
	}()
	return r
}

// CancelSubject is where a caller says it gave up on call callID of
// service; the server holding it cancels its context.
func CancelSubject(service, callID string) string {
	return Prefix + "." + service + ".cancel." + callID
}

// cancels are the calls in flight on one server, by call id.
type cancels struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

func (c *cancels) add(id string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]context.CancelFunc{}
	}
	c.m[id] = cancel
}

func (c *cancels) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, id)
}

func (c *cancels) cancel(id string) {
	c.mu.Lock()
	f := c.m[id]
	c.mu.Unlock()
	if f != nil {
		f()
	}
}
