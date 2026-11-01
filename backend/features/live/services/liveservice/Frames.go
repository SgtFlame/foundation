package liveservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/live"
)

// The frames of the live connection (documentation/Live.md › The live
// connection). Every frame is a JSON object whose "type" says what it is;
// a frame that causes work may carry a "trace".

// Head is what every frame starts with.
type Head struct {
	Type  string `json:"type"`
	Trace string `json:"trace,omitempty"`
}

// CommandFrame → sends a command; ID is its idempotency key.
type CommandFrame struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Target  string          `json:"target"`
	Args    json.RawMessage `json:"args,omitempty"`
	Request string          `json:"request,omitempty"`
	Trace   string          `json:"trace,omitempty"`
}

// AckFrame ← a committed command.
type AckFrame struct {
	Type      string `json:"type"` // ack
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Seq       uint64 `json:"seq"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// RejectedFrame ← a command that committed nothing.
type RejectedFrame struct {
	Type      string         `json:"type"` // rejected
	ID        string         `json:"id"`
	Code      uint16         `json:"code"`
	Message   string         `json:"message"`
	ErrorCode string         `json:"errorCode,omitempty"`
	Problems  []live.Problem `json:"problems,omitempty"`
}

// SubscribeFrame → opens a query; After resumes from a position.
type SubscribeFrame struct {
	Sub    string          `json:"sub"`
	Query  string          `json:"query"`
	Params json.RawMessage `json:"params,omitempty"`
	After  live.Position   `json:"after,omitempty"`
	Trace  string          `json:"trace,omitempty"`
}

// UnsubscribeFrame → closes one.
type UnsubscribeFrame struct {
	Sub string `json:"sub"`
}

// SnapshotFrame ← the query's whole result.
type SnapshotFrame struct {
	Type string        `json:"type"` // snapshot
	Sub  string        `json:"sub"`
	Pos  live.Position `json:"pos"`
	Data any           `json:"data"`
}

// ChangeFrame ← one change.
type ChangeFrame struct {
	Type  string        `json:"type"` // change
	Sub   string        `json:"sub"`
	Pos   live.Position `json:"pos"`
	Delta live.Delta    `json:"delta"`
}

// SubErrorFrame ← a subscription that failed and is closed.
type SubErrorFrame struct {
	Type      string `json:"type"` // subError
	Sub       string `json:"sub"`
	Code      uint16 `json:"code"`
	Message   string `json:"message"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// PresenceFrame → this connection's presence in an object; a null State
// leaves it. Who is there comes from the live.presence query.
type PresenceFrame struct {
	Object string          `json:"object"`
	State  json.RawMessage `json:"state,omitempty"`
	Leave  bool            `json:"leave,omitempty"`
}

// CallFrame → starts a call; CancelFrame → stops it.
type CallFrame struct {
	ID    string          `json:"id"`
	Call  string          `json:"call"`
	Args  json.RawMessage `json:"args,omitempty"`
	Trace string          `json:"trace,omitempty"`
}

type CancelFrame struct {
	ID string `json:"id"`
}

// EventFrame ← one event of a call's stream.
type EventFrame struct {
	Type  string `json:"type"` // event
	ID    string `json:"id"`
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// EndFrame ← the end of a call: Code 200, or the failure.
type EndFrame struct {
	Type      string `json:"type"` // end
	ID        string `json:"id"`
	Code      uint16 `json:"code"`
	Message   string `json:"message,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// HelloFrame ← the first frame: the connection's id.
type HelloFrame struct {
	Type       string `json:"type"` // hello
	Connection string `json:"connection"`
	UserID     uint64 `json:"userId,string"`
	Color      string `json:"color"`
}
