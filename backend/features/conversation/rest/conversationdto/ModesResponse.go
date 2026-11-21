package conversationdto

import (
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ModesResponse struct {
	rest.Status
	Modes []ModeInfo `json:"modes"`
}

type ModeInfo struct {
	ID          string              `json:"id"`
	Label       string              `json:"label"`
	Icon        string              `json:"icon,omitempty"`
	GraphRef    string              `json:"graphRef"`
	Default     bool                `json:"default,omitempty"`
	Focus       *conversation.Focus `json:"focus,omitempty"`
	Description string              `json:"description,omitempty"`
	Settings    []string            `json:"settings,omitempty"` // the context settings its graph uses; empty uses them all
	Missing     bool                `json:"missing,omitempty"`  // its graph does not exist
}

// ModesResponseFrom converts the service's response; its code is carried
// by the HTTP status rather than the body.
func ModesResponseFrom(resp conversationservice.ModesResponse) ModesResponse {
	out := ModesResponse{Status: rest.StatusFrom(resp.Common), Modes: []ModeInfo{}}
	for _, m := range resp.Modes {
		out.Modes = append(out.Modes, ModeInfo{ID: m.ID, Label: m.Label, Icon: m.Icon, GraphRef: m.GraphRef, Default: m.Default,
			Focus: m.Focus, Description: m.Description, Settings: m.Settings, Missing: m.Missing})
	}
	return out
}
