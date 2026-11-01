package uidto

import "encoding/json"

// The ui.* frames the browser sends on the live connection; the server's
// (ui.tree, ui.patch, ui.error) are ui.TreeFrame, ui.PatchFrame and
// ui.ErrorFrame.

// OpenFrame → ui.open: opens (or resumes) Session, an id the browser
// chose and keeps in its tab, on Page.
type OpenFrame struct {
	Session string          `json:"session"`
	Page    string          `json:"page"`
	Params  json.RawMessage `json:"params,omitempty"`
	Trace   string          `json:"trace,omitempty"`
}

// EventFrame → ui.event: a component event on the node at Path.
type EventFrame struct {
	Session string         `json:"session"`
	Handler string         `json:"handler"`
	Path    []string       `json:"path"`
	Payload map[string]any `json:"payload,omitempty"`
	Rev     int64          `json:"rev"`
	Trace   string         `json:"trace,omitempty"`
}

// CloseFrame → ui.close: its tab was closed.
type CloseFrame struct {
	Session string `json:"session"`
}
