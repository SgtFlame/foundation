package tools

import (
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Status is where a call is.
type Status string

const (
	StatusRunning  Status = "running"  // a server tool is running
	StatusAwaiting Status = "awaiting" // waiting for the user to approve it
	StatusClient   Status = "client"   // waiting for the browser to run it (or the user to answer it)
	StatusDone     Status = "done"
	StatusFailed   Status = "failed" // the tool failed, or its arguments did not match its schema
	StatusDenied   Status = "denied" // the user (or the run's policy) refused it
	StatusTimeout  Status = "timeout"
)

// Ended reports whether a call with this status is finished.
func (s Status) Ended() bool {
	return s != StatusRunning && s != StatusAwaiting && s != StatusClient
}

// Approval is how a call was allowed.
type Approval string

const (
	ApprovalAuto     Approval = "auto"     // its effect does not ask
	ApprovalApproved Approval = "approved" // the user approved this call
	ApprovalAlways   Approval = "always"   // allowed for the conversation, now or earlier
	ApprovalDenied   Approval = "denied"
)

// ToolCall records one call, linked to its round or Cognition run, and is
// stored when it ends; a chat round's calls are shown again with it.
type ToolCall struct {
	db.Model
	db.Tenanted
	db.Owned
	ConversationID uint64 `gorm:"index"`
	RoundID        uint64 `gorm:"index"`
	RunID          uint64 `gorm:"index"`
	NodeID         string
	Step           int    // the loop's step, from 1
	CallID         string // the call's ID in its round
	Name           string // the tool's name
	Title          string
	Effect         Effect
	Location       Location
	Args           string // JSON
	Status         Status
	Approval       Approval
	Summary        string // the result's one line of markdown
	Result         string // the JSON the model saw, cut to the tools section's maxResultKB
	ResultBytes    int    // its size before it was cut
	Error          string // why it failed or was denied, as the model was told
	Cited          string // JSON: the numbers [n] of the citations the result added
	DurationMs     int64
}

// Models are the tools feature's tables, for db.Migrate.
func Models() []any {
	return []any{&ToolCall{}}
}
