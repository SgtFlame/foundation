package chronicledto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type GetResponse struct {
	rest.Status
	Entry  *Entry        `json:"entry,omitempty"`
	Links  []Link        `json:"links,omitempty"`
	Rounds []LinkedRound `json:"rounds,omitempty"` // newest first
}

// LinkedRound is a round that created or updated the entry.
type LinkedRound struct {
	RoundID           uint64          `json:"roundId,string"`
	ConversationID    uint64          `json:"conversationId,string"`
	ConversationTitle string          `json:"conversationTitle,omitempty"`
	Time              time.Time       `json:"time"`
	Action            string          `json:"action"`             // created or updated
	Text              string          `json:"text"`               // the round's message, cut short
	Previous          json.RawMessage `json:"previous,omitempty"` // an update's earlier {shortSummary, fullSummary}
}

// GetResponseFrom converts the service's GetResponse; its code is carried by
// the HTTP status rather than the body.
func GetResponseFrom(resp chronicleservice.GetResponse) GetResponse {
	out := GetResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Entry.ID == 0 {
		return out
	}
	entry := EntryFrom(resp.Entry, true)
	out.Entry, out.Links = &entry, linksFrom(resp.Links)
	for _, r := range resp.Rounds {
		lr := LinkedRound{RoundID: r.RoundID, ConversationID: r.ConversationID, ConversationTitle: r.ConversationTitle, Time: r.CreatedAt, Action: r.Action, Text: r.Text}
		if r.Previous != "" {
			lr.Previous = json.RawMessage(r.Previous)
		}
		out.Rounds = append(out.Rounds, lr)
	}
	return out
}
