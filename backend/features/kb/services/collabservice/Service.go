package collabservice

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Service lets several users edit one volume file together. Clients sync a Yjs
// document over the y-websocket protocol; the service relays it without
// decoding the CRDT, and one elected client (the writer) seeds and saves it.
type Service interface {
	OpenFile(args OpenFileArgs) OpenFileResponse
	Collaborate(args CollaborateArgs) CollaborateResponse
}

// Conn is one client's message stream, independent of the transport.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, msg []byte) error
}

type OpenFileArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath // the folders down to the file's name, without its extension
	Type string               // content type, e.g. "markdown"
}

type OpenFileResponse struct {
	Common features.ServiceResponse
	FileID uint64 // the collaboration room; every user opening the same file gets the same ID
}

type CollaborateArgs struct {
	features.ServiceArgs
	Ctx    context.Context
	FileID uint64
	Conn   Conn
}

// CollaborateResponse is returned once the connection ends.
type CollaborateResponse struct {
	Common features.ServiceResponse
}
