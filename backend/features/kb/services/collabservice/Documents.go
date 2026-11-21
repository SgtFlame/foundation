package collabservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Document is text a room edits other than a volume file (forge 07l): a
// record's markdown field. Its kind, a DocumentKind another feature
// contributes, opens it. The room seeds the shared text from Load and the
// writer's saves go through Save, as the writer's principal.
type Document interface {
	// Key names the document: everyone opening the same one shares a room.
	Key() string
	// Name is how logs and errors name it.
	Name() string
	// Load is its text now, read as args' principal.
	Load(args features.ServiceArgs) (string, error)
	// Save stores text as args' principal.
	Save(args features.ServiceArgs, text string) error
	// Access reports whether args' principal may read it (nil) or not
	// (the failure), and whether they may only read it.
	Access(args features.ServiceArgs) (readOnly bool, failed *features.ServiceResponse)
}

// DocumentKind opens documents of one kind from a reference, e.g.
// {"entity": "note", "id": "…", "field": "body"} for kind "record".
type DocumentKind struct {
	extensions.Descriptor // ID is the kind
	Description           string
	Open                  func(args features.ServiceArgs, ref json.RawMessage) (Document, *features.ServiceResponse)
}

// DocumentKinds is the collab.document point.
var DocumentKinds = extensions.NewPoint[DocumentKind]("collab.document", "Kinds of text other than volume files that collaboration rooms edit, such as a record's markdown field")

// OpenDocArgs opens the room of a document of a kind.
type OpenDocArgs struct {
	features.ServiceArgs
	Kind string
	Ref  json.RawMessage
}
