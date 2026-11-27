package liveservice

import (
	"context"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Service serves live connections: one WebSocket per browser window
// carrying commands, subscriptions, presence, calls and the frames of the
// live.extension contributions (UI sessions, collaboration rooms).
type Service interface {
	//svc:local
	Serve(args ServeArgs) ServeResponse

	// The live core for Go callers in other processes (forge 21d-4):
	// what a feature did with the bus, the store and the subscriptions
	// through its own JetStream handle.

	// Execute decides and commits one command as the args' principal,
	// as bus.Execute does (the command id makes a retry a duplicate).
	//svc:idempotent
	Execute(args ExecuteArgs) ExecuteResponse
	// Transact decides ops in turn and commits them as one atomic batch,
	// as bus.Transact does.
	//svc:idempotent
	Transact(args TransactArgs) TransactResponse
	// Read is one projected key, for a service principal.
	//svc:idempotent
	Read(args ReadArgs) ReadResponse
	// Subscribe opens a query as the args' principal and delivers its
	// snapshot and changes until the args' context ends.
	//svc:stream
	Subscribe(args SubscribeArgs) *SubscribeRequest
}

// Command is one command as sent (bus.Request without its ServiceArgs).
type Command struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Target    string          `json:"target,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	App       string          `json:"app,omitempty"`
	Actor     string          `json:"actor,omitempty"`
	Undoes    string          `json:"undoes,omitempty"`
	Branch    string          `json:"branch,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
}

// Committed is a commit, or the problems that refused it (bus.Result).
type Committed struct {
	Stream    string         `json:"stream,omitempty"`
	Target    string         `json:"target,omitempty"`
	Seq       uint64         `json:"seq,omitempty"`
	Duplicate bool           `json:"duplicate,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	Problems  []live.Problem `json:"problems,omitempty"`
}

type ExecuteArgs struct {
	features.ServiceArgs
	Command Command
}

type ExecuteResponse struct {
	Common features.ServiceResponse
	Committed
}

type TransactArgs struct {
	features.ServiceArgs
	Command Command // the request: ID, RequestID, App, Actor, Undoes, Branch
	Ops     []live.Op
	DryRun  bool
}

// DecidedOp is one op of a transaction as decided (bus.Decided).
type DecidedOp struct {
	Op     live.Op `json:"op"`
	Events int     `json:"events"`
}

type TransactResponse struct {
	Common features.ServiceResponse
	Committed
	Ops []DecidedOp
}

type ReadArgs struct {
	features.ServiceArgs
	Projector string
	Key       string
}

type ReadResponse struct {
	Common features.ServiceResponse
	Found  bool
	Value  live.Projected
}

type SubscribeArgs struct {
	features.ServiceArgs // its context ends the subscription
	Query                string
	Params               json.RawMessage
}

// SubscribeEvent is one result of a subscription: a Snapshot (Data), a
// Change (Delta) or the failure that ended it (Failed).
type SubscribeEvent struct {
	Snapshot bool                      `json:"snapshot,omitempty"`
	Data     json.RawMessage           `json:"data,omitempty"`
	Delta    *live.Delta               `json:"delta,omitempty"`
	Position live.Position             `json:"position,omitempty"`
	Failed   *features.ServiceResponse `json:"failed,omitempty"`
}

// SubscribeRequest delivers the subscription's results in order; Events
// closes when it ends.
type SubscribeRequest struct {
	Events <-chan SubscribeEvent
}

// Socket is one connection's message stream, independent of the transport:
// JSON text frames, and binary frames whose first four bytes are a channel.
type Socket interface {
	Read(ctx context.Context) (binary bool, msg []byte, err error)
	Write(ctx context.Context, binary bool, msg []byte) error
}

type ServeArgs struct {
	features.ServiceArgs // the connection's principal; Ctx ends it
	Socket               Socket
}

// ServeResponse is returned once the connection ends.
type ServeResponse struct {
	Common features.ServiceResponse
}
