package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type RoundsResponse struct {
	rest.Status
	Rounds []Round `json:"rounds,omitempty"` // oldest first
}

// RoundsResponseFrom converts the service's RoundsResponse; its code is
// carried by the HTTP status rather than the body.
func RoundsResponseFrom(resp conversationservice.RoundsResponse) RoundsResponse {
	out := RoundsResponse{Status: rest.StatusFrom(resp.Common)}
	byRound := map[uint64][]RoundSegment{}
	for _, seg := range resp.Segments {
		byRound[seg.RoundID] = append(byRound[seg.RoundID], RoundSegmentFrom(seg))
	}
	calls := map[uint64][]ToolCall{}
	for _, c := range resp.ToolCalls {
		calls[c.RoundID] = append(calls[c.RoundID], ToolCallFrom(c))
	}
	for _, r := range resp.Rounds {
		round := RoundFrom(r)
		round.Segments = byRound[r.ID]
		round.ToolCalls = calls[r.ID]
		out.Rounds = append(out.Rounds, round)
	}
	return out
}
