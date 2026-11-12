package uidto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
)

// Component is one primitive's metadata.
type Component struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Tags        []string          `json:"tags,omitempty"`
	Props       json.RawMessage   `json:"props"`
	Events      []ui.Event        `json:"events"`
	Slots       []string          `json:"slots,omitempty"`
	Children    bool              `json:"children"`
	Examples    []json.RawMessage `json:"examples"`
}

func ComponentFrom(c ui.Component) Component {
	events := c.Events
	if events == nil {
		events = []ui.Event{}
	}
	return Component{ID: c.ID, Title: c.Title, Description: c.Description, Category: c.Category, Tags: c.Tags, Props: c.Props, Events: events, Slots: c.Slots, Children: c.Children, Examples: c.Examples}
}

type ComponentsResponse struct {
	rest.Status
	Components []Component `json:"components"`
}

func ComponentsResponseFrom(resp uiservice.ComponentsResponse) ComponentsResponse {
	out := ComponentsResponse{Status: rest.StatusFrom(resp.Common), Components: []Component{}}
	for _, c := range resp.Components {
		out.Components = append(out.Components, ComponentFrom(c))
	}
	return out
}
