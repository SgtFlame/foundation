package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// WorkspacesRequest moves a conversation to another of the caller's
// workspaces, or sets the others it is shown in (forge 16d: Move to…,
// Show in…).
type WorkspacesRequest struct {
	ConversationID uint64    `json:"conversationId,string"`
	Move           string    `json:"move,omitempty"`    // the workspace it now belongs to
	ShownIn        *[]string `json:"shownIn,omitempty"` // every workspace it is shown in besides its own; left out keeps them
}

// ToArgs converts the request to the service's SetWorkspacesArgs.
func (r WorkspacesRequest) ToArgs(args features.ServiceArgs) conversationservice.SetWorkspacesArgs {
	return conversationservice.SetWorkspacesArgs{ServiceArgs: args, ConversationID: r.ConversationID, Move: r.Move, ShownIn: r.ShownIn}
}

type WorkspacesResponse struct {
	rest.Status
	Conversation *Conversation `json:"conversation,omitempty"` // with its workspace and shownIn
}

// WorkspacesResponseFrom converts the service's SetWorkspacesResponse.
func WorkspacesResponseFrom(resp conversationservice.SetWorkspacesResponse) WorkspacesResponse {
	out := WorkspacesResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Conversation.ID != 0 {
		c := ConversationFrom(resp.Conversation).WithPlaces(resp.Places)
		out.Conversation = &c
	}
	return out
}
