package msgtest

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/messaging"
)

var log = logging.In("messaging").Tags("msgtest")

// CallID is the live call that drives a test server connection.
const CallID = "msgtest.call"

// CallArgs are msgtest.call's arguments.
type CallArgs struct {
	Connection string          `json:"connection"`
	Control    bool            `json:"control"` // msgtest.control@1, else chat.messaging@1
	Operation  string          `json:"operation"`
	Args       json.RawMessage `json:"args"`
}

// Call runs one operation on a Messaging test server connection through the
// connector bus, as the caller, who must be allowed to use it.
func Call(args features.ServiceArgs, d connections.Deps, a CallArgs) (json.RawMessage, error) {
	c, ok, err := connections.Get(args.Context(), d.Projections, args.Principal.TenantID, a.Connection)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, log.Code("connection_not_found").Errorf("there is no connection %s", a.Connection)
	}
	if c.Type != TypeID {
		return nil, log.Code("not_msgtest").Errorf("connection %s is not a Messaging test server", a.Connection)
	}
	if err := d.CheckUse(args, c); err != nil {
		return nil, err
	}
	contract := messaging.ContractRef
	if a.Control {
		contract = ControlRef
	}
	if len(a.Args) == 0 {
		a.Args = json.RawMessage(`{}`)
	}
	out, e := d.Dispatcher.Call(args, connectors.Call{Contract: contract, Operation: a.Operation, Connection: a.Connection, Args: a.Args})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// ContributeLive registers msgtest.call, for `forge msgtest` and the test
// server's page: one operation on a test server connection, ending with
// one result event (the operation's result) or the connector's error.
func ContributeLive(reg *extensions.Registry, deps func() connections.Deps) {
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: CallID, Source: TypeID}, Title: "Drive the Messaging test server",
		Description: "Runs one operation on a Messaging test server connection: {connection, control, operation, args}, control choosing msgtest.control@1 (post, edit, delete, react, addChannel, fail, expireCursor, dropNotifications, state) over chat.messaging@1. Ends with one result event, the operation's result.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a CallArgs
			if err := json.Unmarshal(c.Args, &a); err != nil || a.Connection == "" || a.Operation == "" {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").Errorf("msgtest.call needs a connection and an operation"))
			}
			out, err := Call(c.ServiceArgs, deps(), a)
			if err != nil {
				if ce, ok := err.(*connectors.Error); ok {
					return c.Fail(http.StatusBadGateway, log.Code(ce.Code).Errorf("%s", ce.Message))
				}
				return c.Fail(http.StatusForbidden, err)
			}
			c.Emit("result", out)
			return nil
		},
	})
}
