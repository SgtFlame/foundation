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
	// EditFile is Go-only (the kb.edit tool calls it), so it has no route.
	EditFile(args EditFileArgs) EditFileResponse
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
	// ReadOnly is set when the principal may read the Volume but not write
	// it: the room drops their edits, so the editor opens read-only.
	ReadOnly bool
}

type CollaborateArgs struct {
	features.ServiceArgs
	FileID uint64
	Conn   Conn
}

// CollaborateResponse is returned once the connection ends.
type CollaborateResponse struct {
	Common features.ServiceResponse
}

// EditFileArgs applies find/replace edits to a file, in order. While the
// file is open the room's writer applies them to the shared document, so
// every editor sees them at once; otherwise the file itself is changed.
type EditFileArgs struct {
	features.ServiceArgs
	Path  kbservice.VolumePath // the folders down to the file's name, without its extension
	Type  string               // content type, e.g. "markdown"
	Edits []Edit
}

type EditFileResponse struct {
	Common features.ServiceResponse
	Live   bool // applied to the open document rather than the file
}
