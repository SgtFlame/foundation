package conversationdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

type SendRequest struct {
	ConversationID  uint64                     `json:"conversationId,string,omitempty"` // omitted starts a new conversation
	Text            string                     `json:"text"`
	Level           string                     `json:"level,omitempty"`     // the default level when omitted
	Mode            string                     `json:"mode,omitempty"`      // a chat.modes id; the conversation's, else the default, when omitted
	Workbench       json.RawMessage            `json:"workbench,omitempty"` // the workbench snapshot
	ContextSettings map[string]json.RawMessage `json:"contextSettings,omitempty"`
	ClientTools     []ClientTool               `json:"clientTools,omitempty"`   // the chat.clientTool contributions the model may call
	Offered         []string                   `json:"offered,omitempty"`       // the chat.modes ids offered where it was sent (forge 15f)
	ModeChosen      bool                       `json:"modeChosen,omitempty"`    // the person chose mode for this message: no pre-mode
	Undoes          uint64                     `json:"undoes,string,omitempty"` // a round this message runs again, which leaves the history (Undo of a switch)
}

// ToArgs converts the request to the service's SendArgs.
func (r SendRequest) ToArgs(args features.ServiceArgs) conversationservice.SendArgs {
	out := conversationservice.SendArgs{
		ServiceArgs: args, ConversationID: r.ConversationID, Text: r.Text, Level: r.Level, Mode: r.Mode,
		Workbench: r.Workbench, ContextSettings: r.ContextSettings, Offered: r.Offered, ModeChosen: r.ModeChosen, Undoes: r.Undoes,
	}
	for _, t := range r.ClientTools {
		out.ClientTools = append(out.ClientTools, t.ToTool())
	}
	return out
}
