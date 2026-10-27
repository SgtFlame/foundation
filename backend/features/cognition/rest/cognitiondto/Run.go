package cognitiondto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
)

// Run is one run of a graph.
type Run struct {
	ID             uint64          `json:"id,string"`
	GraphID        string          `json:"graphId"`
	GraphVersion   int             `json:"graphVersion"`
	Event          string          `json:"event"` // the event node's ID
	Trigger        string          `json:"trigger"`
	Status         string          `json:"status"` // running, ok or failed
	Error          string          `json:"error,omitempty"`
	FailedNode     string          `json:"failedNode,omitempty"`
	ConversationID uint64          `json:"conversationId,string,omitempty"`
	RoundID        uint64          `json:"roundId,string,omitempty"`
	StartedAt      time.Time       `json:"startedAt"`
	DurationMs     int64           `json:"durationMs"`
	Steps          int             `json:"steps"`
	Outputs        json.RawMessage `json:"outputs,omitempty"`
}

// RunFrom converts a run.
func RunFrom(r cognition.Run) Run {
	out := Run{ID: r.ID, GraphID: r.GraphID, GraphVersion: r.GraphVersion, Event: r.Event, Trigger: r.Trigger, Status: r.Status,
		Error: r.Error, FailedNode: r.FailedNode, ConversationID: r.ConversationID, RoundID: r.RoundID,
		StartedAt: r.StartedAt, DurationMs: r.DurationMs, Steps: r.Steps}
	if r.Outputs != "" {
		out.Outputs = json.RawMessage(r.Outputs)
	}
	return out
}
