package liveservice

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features"
)

// Service serves live connections: one WebSocket per browser window
// carrying commands, subscriptions, presence, calls and the frames of the
// live.extension contributions (UI sessions, collaboration rooms).
type Service interface {
	Serve(args ServeArgs) ServeResponse
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
