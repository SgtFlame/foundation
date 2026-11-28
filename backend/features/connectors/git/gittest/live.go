package gittest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("connectors").Tags("gittest")

// ContributeLive registers what Forge's Git test server page and `forge
// gittest` use (forge 24a): the call gittest.control, which sends one
// gittest.control@1 operation to a test repository's connection, and the
// query gittest.log, a repository's branches, log and pull requests kept
// current. Both go through the connector bus as the caller, so the
// dispatcher checks the caller may use the connection.
func ContributeLive(reg *extensions.Registry, dispatcher func() *connectors.Dispatcher) {
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: "gittest.control", Source: TypeID}, Title: "Drive the Git test server",
		Description: "Sends one gittest.control@1 operation (push, fail, protect, merge, log) to a Git test server connection: {connection, operation, args}. Ends with one result event, the operation's result, or fails with the connector error.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a struct {
				Connection string          `json:"connection"`
				Operation  string          `json:"operation"`
				Args       json.RawMessage `json:"args"`
			}
			if err := json.Unmarshal(c.Args, &a); err != nil || a.Connection == "" || a.Operation == "" {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("gittest.control needs a connection and an operation"))
			}
			if len(a.Args) == 0 || string(a.Args) == "null" {
				a.Args = json.RawMessage(`{}`)
			}
			res, e := dispatcher().Call(c.ServiceArgs, connectors.Call{Contract: ControlRef, Operation: a.Operation, Connection: a.Connection, Args: a.Args})
			if e != nil {
				return c.Fail(Status(e), log.Code(e.Code).Public(e.Message).Errorf("%s", e.Message))
			}
			c.Emit("result", res)
			return nil
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "gittest.log", Source: TypeID}, Title: "Git test repository",
		Description: "A Git test server repository (a gittest.repo connection) as gittest.control's log gives it, kept current: {defaultBranch, branches: [{name, sha, protected}], commits: [{sha, parents, message, author, time, paths}] of the branch, newest first, pullRequests: [{number, state, title, head, base}], faults}.",
		Params: json.RawMessage(`{"type":"object","properties":{
  "connection":{"type":"string","description":"The gittest.repo connection's id"},
  "branch":{"type":"string","description":"The branch whose commits to list; the default branch when left out"},
  "limit":{"type":"integer","minimum":1,"description":"The most commits; 50 when left out"}},"required":["connection"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"connection":"site","branch":"main"}`)},
		Action:   "connection.read",
		Run: func(f live.Feed) error {
			var p struct {
				Connection string `json:"connection"`
				Branch     string `json:"branch,omitempty"`
				Limit      int    `json:"limit,omitempty"`
			}
			_ = json.Unmarshal(f.Params(), &p)
			args, _ := json.Marshal(map[string]any{"branch": p.Branch, "limit": p.Limit})
			read := func() (json.RawMessage, *connectors.Error) {
				return dispatcher().Call(f.Args(), connectors.Call{Contract: ControlRef, Operation: "log", Connection: p.Connection, Args: args})
			}
			last, e := read()
			if e != nil {
				return log.Code(e.Code).Errorf("%s", e.Message)
			}
			f.Snapshot(last, live.Position{})
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-f.Context().Done():
					return nil
				case <-tick.C:
				}
				raw, e := read()
				if e != nil || bytes.Equal(raw, last) {
					continue
				}
				last = raw
				f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
			}
		},
	})
}

// Status is the HTTP status of a connector error.
func Status(e *connectors.Error) uint16 {
	switch e.Code {
	case connectors.CodeAuth:
		return http.StatusForbidden
	case connectors.CodeNotFound:
		return http.StatusNotFound
	case connectors.CodeConflict:
		return http.StatusConflict
	case connectors.CodeInvalid:
		return http.StatusBadRequest
	case connectors.CodeRateLimited:
		return http.StatusTooManyRequests
	case connectors.CodeNotSupported:
		return http.StatusNotImplemented
	}
	return http.StatusServiceUnavailable
}
