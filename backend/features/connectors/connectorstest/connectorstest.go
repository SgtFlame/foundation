// Package connectorstest is the test contract and builtin test types of
// the connector registry (forge 07b), for tests only: test.echo@1, served
// by test.echo (no connection), test.secure (a connection with a secret
// password and an event source scripted by its settings) and
// test.exclusive (the same, one session at a time), all implemented here.
package connectorstest

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContractRef is the test contract's reference.
const ContractRef = "test.echo@1"

// Contract is the test contract: echo (read), fail (write, destructive,
// returns the typed error it is asked for), start (send, keyed by id, its
// finished callback correlated by jobId), and the finished event, pushed
// and polled.
func Contract() connectors.Contract {
	return connectors.Contract{
		ID: "test.echo", Version: 1,
		Meta: connectors.Meta{Title: "Test echo", Description: "A test API: echoes text, fails on request and finishes jobs later. For tests only.", Tags: []string{"test"}},
		Operations: []connectors.Operation{
			{
				ID: "echo", Title: "Echo", Description: "Gives the text back, repeated, with its length and the adapter instance that served it",
				Effect: connectors.EffectRead,
				Args: json.RawMessage(`{"type":"object","properties":{
  "text":{"type":"string","description":"The text to echo"},
  "repeat":{"type":"integer","minimum":1,"maximum":10,"description":"How many times; once when left out"},
  "options":{"type":"object","description":"Nested options, one pin","properties":{"upper":{"type":"boolean","description":"In capitals"}}}},"required":["text"]}`),
				Result: json.RawMessage(`{"type":"object","properties":{
  "text":{"type":"string","description":"The echoed text"},
  "length":{"type":"integer","description":"Its length in bytes"},
  "by":{"type":"string","description":"The adapter instance that served the call"}},"required":["text","length"]}`),
				Examples: []connectors.Example{{Title: "Twice", Args: json.RawMessage(`{"text":"hi","repeat":2}`), Result: json.RawMessage(`{"text":"hihi","length":4,"by":"a"}`)}},
				Errors:   []string{connectors.CodeInvalid},
			},
			{
				ID: "fail", Title: "Fail", Description: "Fails with the typed error asked for, for testing error paths",
				Effect: connectors.EffectWrite, Destructive: true,
				Args: json.RawMessage(`{"type":"object","properties":{
  "code":{"type":"string","enum":["auth","notFound","conflict","invalid","rateLimited","unavailable","notSupported"],"description":"The error code to fail with"},
  "message":{"type":"string","description":"The error's message"}},"required":["code"]}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Never: it always fails"}}}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{"code":"notFound","message":"no such thing"}`), Result: json.RawMessage(`{"ok":false}`)}},
				Errors:   connectors.Codes,
			},
			{
				ID: "start", Title: "Start a job", Description: "Starts a job outside; its finished event answers it later, with the same job id",
				Effect: connectors.EffectSend, Key: "id",
				Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The idempotency key: a start with an id already started starts nothing"},
  "text":{"type":"string","description":"What the job works on"}},"required":["id"]}`),
				Result:    json.RawMessage(`{"type":"object","properties":{"jobId":{"type":"string","description":"The started job's id, which its finished event names"}},"required":["jobId"]}`),
				Examples:  []connectors.Example{{Args: json.RawMessage(`{"id":"k1","text":"export"}`), Result: json.RawMessage(`{"jobId":"j-k1"}`)}},
				Callbacks: []connectors.CallbackDef{{Event: "finished", Correlate: connectors.Correlate{Result: "jobId", Event: "jobId"}}},
			},
			{
				ID: "check", Title: "Check", Description: "Signs in and does nothing: whether the connection's settings work",
				Effect:   connectors.EffectRead,
				Args:     json.RawMessage(`{"type":"object","properties":{}}`),
				Result:   json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","description":"Always true: a failure is an error"}}}`),
				Examples: []connectors.Example{{Args: json.RawMessage(`{}`), Result: json.RawMessage(`{"ok":true}`)}},
				Errors:   []string{connectors.CodeAuth, connectors.CodeUnavailable},
			},
		},
		Events: []connectors.EventDef{{
			ID: "finished", Title: "Job finished", Description: "A job started by start finished. Read the job's state rather than trust the order of events.",
			Payload: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The event's own id, unique"},
  "jobId":{"type":"string","description":"The job that finished"},
  "at":{"type":"string","description":"When, RFC 3339, by the service's clock"}},"required":["id","jobId"]}`),
			Example:      json.RawMessage(`{"id":"evt_1","jobId":"j-k1","at":"2026-11-11T10:00:00Z"}`),
			IDFields:     []string{"id"},
			Delivery:     []string{connectors.DeliveryWatch, connectors.DeliveryCallback},
			Subscription: "job.finished",
		}},
	}
}

// Types are the builtin test types.
func Types() []connectors.Type {
	return []connectors.Type{
		{ID: "test.echo", Contracts: []string{ContractRef}, NoConnection: true, Source: connectors.SourceBuiltin,
			Meta: connectors.Meta{Title: "Test echo", Description: "Implements test.echo@1 in Go with no connection. For tests only.", Tags: []string{"test"}}},
		{ID: "test.secure", Contracts: []string{ContractRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings),
			Meta: connectors.Meta{Title: "Test secure echo", Description: "Implements test.echo@1 over a connection with a secret password. For tests only.", Tags: []string{"test"}}},
		{ID: "test.exclusive", Contracts: []string{ContractRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(settings), Exclusive: true,
			Meta: connectors.Meta{Title: "Test exclusive echo", Description: "Implements test.echo@1 over a connection that allows one session at a time. For tests only.", Tags: []string{"test"}}},
	}
}

// settings is the connection types' settings schema: a user and secret
// password, and the event source's script: Watch emits finished events 1
// to events after its cursor (storing no cursor from loseCursorFrom on),
// then returns, or waits when push; failWatch makes it fail with that code.
const settings = `{"type":"object","properties":{
  "user":{"type":"string","title":"User"},
  "password":{"type":"secret","title":"Password"},
  "events":{"type":"integer","title":"Events","description":"How many finished events the source has"},
  "loseCursorFrom":{"type":"integer","title":"Lose cursor from"},
  "push":{"type":"boolean","title":"Push"},
  "failWatch":{"type":"string","title":"Fail watch"}},"required":["user"]}`

// Contribute registers the test contract and types as builtins.
func Contribute(reg *extensions.Registry) {
	connectors.ContributeBuiltins(reg, "test", []connectors.Contract{Contract()}, Types())
}

// Counter counts calls served, by instance.
type Counter struct{ n atomic.Int64 }

// N is how many calls it counted.
func (c *Counter) N() int64 { return c.n.Load() }

// Options are the adapter options that serve both test types as instance
// by; count, when not nil, counts the calls served.
func Options(by string, count *Counter) adapter.Options {
	open := func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if conn.Type != "test.echo" {
			_ = json.Unmarshal(conn.Settings, &st)
			if st.Password == "" || st.Password == "wrong" {
				return nil, connectors.Errorf(connectors.CodeAuth, "connection %s: the password was refused", conn.ID)
			}
		}
		Sessions.open(conn.ID)
		return &session{by: by, count: count, conn: conn.ID, settings: st}, nil
	}
	return adapter.Options{Name: "test-" + by, Types: Types(), Contracts: []connectors.Contract{Contract()},
		Impls: []adapter.Impl{{Type: "test.echo", Open: open}, {Type: "test.secure", Open: open}, {Type: "test.exclusive", Open: open}}}
}

// Settings are a test connection's settings as its session reads them.
type Settings struct {
	User, Password string
	Events         int
	LoseCursorFrom int
	Push           bool
	FailWatch      string
}

// SessionCount counts the sessions open on each connection, in this
// process, and the most ever open at once.
type SessionCount struct {
	mu  sync.Mutex
	now map[string]int
	max map[string]int
}

// Sessions are this process's test sessions.
var Sessions = &SessionCount{now: map[string]int{}, max: map[string]int{}}

func (c *SessionCount) open(conn string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now[conn]++
	c.max[conn] = max(c.max[conn], c.now[conn])
}

func (c *SessionCount) close(conn string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now[conn]--
}

// Max is the most sessions ever open at once on conn.
func (c *SessionCount) Max(conn string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max[conn]
}

type session struct {
	by       string
	count    *Counter
	conn     string
	settings Settings
	closed   atomic.Bool
}

func (s *session) Close() error {
	if !s.closed.Swap(true) {
		Sessions.close(s.conn)
	}
	return nil
}

// Watch is the scripted event source.
func (s *session) Watch(ctx adapter.Context, cursor json.RawMessage, emit func(adapter.Event) error) error {
	if s.settings.FailWatch != "" {
		return connectors.Errorf(s.settings.FailWatch, "the source failed as asked")
	}
	from := 0
	if len(cursor) > 0 {
		_ = json.Unmarshal(cursor, &from)
	}
	for i := from + 1; i <= s.settings.Events; i++ {
		id := fmt.Sprintf("evt_%d", i)
		payload, _ := json.Marshal(map[string]any{"id": id, "jobId": fmt.Sprintf("j-%d", i), "at": "2026-11-11T10:00:00Z"})
		e := adapter.Event{Contract: ContractRef, Name: "finished", ID: id, Payload: payload}
		if s.settings.LoseCursorFrom == 0 || i < s.settings.LoseCursorFrom {
			e.Cursor, _ = json.Marshal(i)
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	if s.settings.Push {
		<-ctx.Done()
	}
	return nil
}

func (s *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	if s.count != nil {
		s.count.n.Add(1)
	}
	switch op {
	case "check":
		if s.conn != "" && s.settings.User == "" {
			return nil, connectors.Errorf(connectors.CodeAuth, "no user")
		}
		return json.RawMessage(`{"ok":true}`), nil
	case "echo" + "-slow":
		time.Sleep(50 * time.Millisecond)
		return json.RawMessage(`{}`), nil
	case "echo":
		var a struct {
			Text    string
			Repeat  int
			Options struct{ Upper bool }
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "%v", err)
		}
		text := strings.Repeat(a.Text, max(a.Repeat, 1))
		if a.Options.Upper {
			text = strings.ToUpper(text)
		}
		return json.Marshal(map[string]any{"text": text, "length": len(text), "by": s.by})
	case "fail":
		var a struct{ Code, Message string }
		_ = json.Unmarshal(args, &a)
		return nil, connectors.Errorf(a.Code, "%s", a.Message)
	case "start":
		var a struct{ ID string }
		_ = json.Unmarshal(args, &a)
		return json.Marshal(map[string]any{"jobId": "j-" + a.ID})
	}
	return nil, connectors.Errorf(connectors.CodeNotSupported, "%s", fmt.Sprintf("no operation %q", op))
}
