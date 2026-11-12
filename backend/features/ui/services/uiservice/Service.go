package uiservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Service runs server-driven UI: the component catalog, the installed
// apps' editor kinds, and UI sessions over the live connection.
type Service interface {
	Components(args ComponentsArgs) ComponentsResponse
	EditorKinds(args EditorKindsArgs) EditorKindsResponse
	// Open, Event and Close are Go-only: the ui.* frames of the live
	// connection call them (uihandlers.ContributeLive).
	Open(args OpenArgs) OpenResponse
	Event(args EventArgs)
	Close(args CloseArgs)
}

type ComponentsArgs struct {
	features.ServiceArgs
}

type ComponentsResponse struct {
	Common     features.ServiceResponse
	Components []ui.Component
}

type EditorKindsArgs struct {
	features.ServiceArgs
}

type EditorKindsResponse struct {
	Common features.ServiceResponse
	Apps   []ui.App
	Kinds  []ui.EditorKind
}

// OpenArgs opens (or resumes) session Session on Page for the connection
// whose frames Send delivers.
type OpenArgs struct {
	features.ServiceArgs
	Session string
	Page    string // "<appId>/<page>"
	Params  json.RawMessage
	Send    func(frame any)
}

type OpenResponse struct {
	Common  features.ServiceResponse
	Session *ui.Session
}

type EventArgs struct {
	features.ServiceArgs
	Session *ui.Session
	Handler string
	Path    []string
	Payload map[string]any
}

// CloseArgs ends a session; Forget removes its record too (its tab was
// closed, rather than its connection lost).
type CloseArgs struct {
	features.ServiceArgs
	Session *ui.Session
	Forget  bool
}
