package cognitiondto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// Step is one node's execution in a run.
type Step struct {
	Seq        int             `json:"seq"`
	NodeID     string          `json:"nodeId"`
	NodeType   string          `json:"nodeType"`
	Context    string          `json:"context,omitempty"`
	Inputs     json.RawMessage `json:"inputs,omitempty"`  // by pin ID; a value cut short is {"$truncated": bytes, "preview": text}
	Outputs    json.RawMessage `json:"outputs,omitempty"` // by pin ID
	ExecPin    string          `json:"execPin,omitempty"`
	StartedAt  time.Time       `json:"startedAt"`
	DurationMs int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
}

// NodeUsage is what a node's model calls used in the run.
type NodeUsage struct {
	NodeID         string `json:"nodeId"`
	Calls          int    `json:"calls"`
	InputTokens    int    `json:"inputTokens"`
	OutputTokens   int    `json:"outputTokens"`
	ThinkingTokens int    `json:"thinkingTokens"`
	CostMicros     int64  `json:"costMicros"`
	Currency       string `json:"currency,omitempty"`
}

type GetTraceResponse struct {
	rest.Status
	Run   *Run        `json:"run,omitempty"`
	Steps []Step      `json:"steps,omitempty"`
	Usage []NodeUsage `json:"usage,omitempty"`
}

// GetTraceResponseFrom converts the service's GetTraceResponse.
func GetTraceResponseFrom(resp cognitionservice.GetTraceResponse) GetTraceResponse {
	out := GetTraceResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Run.ID == 0 {
		return out
	}
	run := RunFrom(resp.Run)
	out.Run = &run
	raw := func(s string) json.RawMessage {
		if s == "" {
			return nil
		}
		return json.RawMessage(s)
	}
	for _, s := range resp.Steps {
		out.Steps = append(out.Steps, Step{Seq: s.Seq, NodeID: s.NodeID, NodeType: s.NodeType, Context: s.Context, Inputs: raw(s.Inputs), Outputs: raw(s.Outputs),
			ExecPin: s.ExecPin, StartedAt: s.StartedAt, DurationMs: s.DurationMs, Error: s.Error})
	}
	for _, u := range resp.Usage {
		out.Usage = append(out.Usage, NodeUsage(u))
	}
	return out
}
