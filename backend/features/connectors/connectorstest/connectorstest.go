// Package connectorstest is the test contract and builtin test types of
// the connector registry (forge 07b), for tests only: test.echo@1, served
// by test.echo (no connection) and test.secure (a connection with a
// secret password), both implemented here.
package connectorstest

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

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
		{ID: "test.secure", Contracts: []string{ContractRef}, Source: connectors.SourceBuiltin,
			Settings: json.RawMessage(`{"type":"object","properties":{"user":{"type":"string","title":"User"},"password":{"type":"secret","title":"Password"}}}`),
			Meta:     connectors.Meta{Title: "Test secure echo", Description: "Implements test.echo@1 over a connection with a secret password. For tests only.", Tags: []string{"test"}}},
	}
}

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
		if conn.Type == "test.secure" {
			var s struct{ User, Password string }
			_ = json.Unmarshal(conn.Settings, &s)
			if s.Password == "" {
				return nil, connectors.Errorf(connectors.CodeAuth, "connection %s has no password", conn.ID)
			}
		}
		return session{by: by, count: count}, nil
	}
	return adapter.Options{Name: "test-" + by, Types: Types(), Contracts: []connectors.Contract{Contract()},
		Impls: []adapter.Impl{{Type: "test.echo", Open: open}, {Type: "test.secure", Open: open}}}
}

type session struct {
	by    string
	count *Counter
}

func (s session) Close() error { return nil }

func (s session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	if s.count != nil {
		s.count.n.Add(1)
	}
	switch op {
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
