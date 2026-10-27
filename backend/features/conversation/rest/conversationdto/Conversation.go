package conversationdto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/conversation"
)

type Conversation struct {
	ID              uint64          `json:"id,string"`
	Created         time.Time       `json:"created"`
	Title           string          `json:"title,omitempty"`
	LastRoundAt     time.Time       `json:"lastRoundAt"`
	ContextSettings json.RawMessage `json:"contextSettings,omitempty"` // by chat.contextSettings ID
}

func ConversationFrom(c conversation.Conversation) Conversation {
	out := Conversation{ID: c.ID, Created: c.CreatedAt, Title: c.Title, LastRoundAt: c.LastRoundAt}
	if c.ContextSettings != "" {
		out.ContextSettings = json.RawMessage(c.ContextSettings)
	}
	return out
}
