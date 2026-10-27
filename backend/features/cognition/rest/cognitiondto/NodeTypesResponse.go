package cognitiondto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// NodeType is a node type as the Palette and the details panel show it.
type NodeType struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Category       string          `json:"category"`
	Description    string          `json:"description"`
	Color          string          `json:"color,omitempty"`
	Source         string          `json:"source"`
	Event          bool            `json:"event,omitempty"`
	Config         json.RawMessage `json:"config,omitempty"`         // the config's schema
	Pins           cognition.Pins  `json:"pins"`                     // its fixed pins
	PinsFromConfig bool            `json:"pinsFromConfig,omitempty"` // its pins follow each node's config: GetGraph has them
}

// DataType is a data type and its colour.
type DataType struct {
	ID          string `json:"id"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type NodeTypesResponse struct {
	rest.Status
	NodeTypes []NodeType `json:"nodeTypes"`
	Types     []DataType `json:"types"`
}

// NodeTypesResponseFrom converts the service's NodeTypesResponse.
func NodeTypesResponseFrom(resp cognitionservice.NodeTypesResponse) NodeTypesResponse {
	out := NodeTypesResponse{Status: rest.StatusFrom(resp.Common), NodeTypes: []NodeType{}, Types: []DataType{}}
	for _, t := range resp.NodeTypes {
		pins := pinsOf(t.Pins)
		out.NodeTypes = append(out.NodeTypes, NodeType{ID: t.ID, Title: t.Title, Category: t.Category, Description: t.Description, Color: t.Color,
			Source: t.Source, Event: t.Event, Config: t.Config, Pins: pins, PinsFromConfig: t.PinsFor != nil})
	}
	for _, t := range resp.Types {
		out.Types = append(out.Types, DataType{ID: t.ID, Color: t.Color, Description: t.Description})
	}
	return out
}
