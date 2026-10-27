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
	Workbench       json.RawMessage            `json:"workbench,omitempty"` // the workbench snapshot
	ContextSettings map[string]json.RawMessage `json:"contextSettings,omitempty"`
	ClientTools     []ClientTool               `json:"clientTools,omitempty"` // the chat.clientTool contributions the model may call
}

// ToArgs converts the request to the service's SendArgs.
func (r SendRequest) ToArgs(args features.ServiceArgs) conversationservice.SendArgs {
	out := conversationservice.SendArgs{
		ServiceArgs: args, ConversationID: r.ConversationID, Text: r.Text, Level: r.Level,
		Workbench: r.Workbench, ContextSettings: r.ContextSettings,
	}
	for _, t := range r.ClientTools {
		out.ClientTools = append(out.ClientTools, t.ToTool())
	}
	return out
}
