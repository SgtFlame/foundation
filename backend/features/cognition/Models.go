package cognition

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// Run statuses.
const (
	StatusRunning = "running"
	StatusOK      = "ok"
	StatusFailed  = "failed"
)

// Run is one run of a graph, from one event. Runs are their principal's.
type Run struct {
	db.Model
	db.Tenanted
	db.Owned
	GraphID        string `gorm:"index"`
	GraphVersion   int
	Event          string // the event node's ID
	Trigger        string // what started it, e.g. "chat"
	Status         string
	Error          string // the failure, safe to show
	FailedNode     string // the node it failed on, if any
	ConversationID uint64 `gorm:"index"`
	RoundID        uint64
	StartedAt      time.Time `gorm:"index"`
	DurationMs     int64
	Steps          int
	Outputs        string // the run's outputs as captured JSON
}

// TableName keeps the table apart from other features' runs.
func (Run) TableName() string { return "cognition_runs" }

// Step is one node's execution in a run: the trace the Log Viewer shows.
type Step struct {
	db.Model
	db.Tenanted
	RunID      uint64 `gorm:"index"`
	Seq        int    // the order the steps ran in, from 1
	NodeID     string
	NodeType   string
	Context    string // the execution context: the loop frames, e.g. "forEach:n5[2]"; empty outside loops
	Inputs     string // captured JSON object, by pin ID
	Outputs    string
	ExecPin    string // the exec pin taken; empty for pure nodes and at the end
	StartedAt  time.Time
	DurationMs int64
	Error      string
}

// TableName names the steps after their runs.
func (Step) TableName() string { return "cognition_run_steps" }

// Models are the Cognition tables, for db.Migrate.
func Models() []any {
	return []any{&Run{}, &Step{}}
}
