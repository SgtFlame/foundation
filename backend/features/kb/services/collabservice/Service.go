package collabservice

import (
	"context"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Service lets several users edit one volume file together. Clients sync a Yjs
// document over the y-websocket protocol; the service relays it without
// decoding the CRDT, and one elected client (the writer) seeds and saves it.
type Service interface {
	OpenFile(args OpenFileArgs) OpenFileResponse
	// OpenDoc returns the room of a document of a kind (Documents.go): a
	// record's markdown field. Its FileID is joined as a file's is.
	OpenDoc(args OpenDocArgs) OpenFileResponse
	//svc:local
	Collaborate(args CollaborateArgs) CollaborateResponse
	// EditFile is Go-only (the kb.edit tool calls it), so it has no route.
	EditFile(args EditFileArgs) EditFileResponse
	// EditDoc applies edits to a document of a kind (forge 18c: a
	// record's markdown field), through its room's writer while it is
	// open, else to the document. Go-only (the Edit text together node).
	EditDoc(args EditDocArgs) EditDocResponse
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

// EditDocArgs applies edits to a document of a kind: find/replace edits,
// or ranges of its saved text (Prepare). The principal must be able to
// write it. Each applies the edits that can apply and reports the others;
// without it one edit that cannot apply fails them all.
type EditDocArgs struct {
	features.ServiceArgs
	Kind  string
	Ref   json.RawMessage
	Edits []Edit
	Each  bool
}

type EditDocResponse struct {
	Common features.ServiceResponse
	Live   bool // applied to the open document rather than saved
	// Applied and Problems tell, per edit given, whether it applied and why not
	Applied  []bool
	Problems []string
}
