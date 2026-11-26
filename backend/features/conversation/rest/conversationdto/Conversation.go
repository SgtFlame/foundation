package conversationdto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

type Conversation struct {
	ID              uint64          `json:"id,string"`
	Created         time.Time       `json:"created"`
	Title           string          `json:"title,omitempty"`
	Mode            string          `json:"mode,omitempty"` // the chat.modes id its rounds run in
	LastRoundAt     time.Time       `json:"lastRoundAt"`
	ContextSettings json.RawMessage `json:"contextSettings,omitempty"` // by chat.contextSettings ID
	// Forge 16d: the workspace it belongs to, and the others it is shown
	// in; empty when the server has no workspaces
	Workspace string   `json:"workspace,omitempty"`
	ShownIn   []string `json:"shownIn,omitempty"`
}

func ConversationFrom(c conversation.Conversation) Conversation {
	out := Conversation{ID: c.ID, Created: c.CreatedAt, Title: c.Title, Mode: c.Mode, LastRoundAt: c.LastRoundAt}
	if c.ContextSettings != "" {
		out.ContextSettings = json.RawMessage(c.ContextSettings)
	}
	return out
}

// WithPlaces adds the workspaces a conversation is listed in (forge 16d).
func (c Conversation) WithPlaces(p conversationservice.Places) Conversation {
	c.Workspace, c.ShownIn = p.Own, p.ShownIn
	return c
}
