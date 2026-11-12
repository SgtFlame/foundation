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
	Example        string          `json:"example"`
	Kinds          []string        `json:"kinds,omitempty"` // the graph kinds it may be used in; empty is every kind
	Effects        bool            `json:"effects,omitempty"`
}

// Kind is a graph kind.
type Kind struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Returns     string          `json:"returns"`
	Events      []string        `json:"events,omitempty"`
	Pure        bool            `json:"pure,omitempty"`
	Spec        json.RawMessage `json:"spec,omitempty"`
}

// Conversion is an implicit conversion an edge may make.
type Conversion struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DataType is a data type and its colour.
type DataType struct {
	ID          string `json:"id"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type NodeTypesResponse struct {
	rest.Status
	NodeTypes   []NodeType   `json:"nodeTypes"`
	Types       []DataType   `json:"types"`
	Kinds       []Kind       `json:"kinds"`
	Conversions []Conversion `json:"conversions"`
}

// NodeTypesResponseFrom converts the service's NodeTypesResponse.
func NodeTypesResponseFrom(resp cognitionservice.NodeTypesResponse) NodeTypesResponse {
	out := NodeTypesResponse{Status: rest.StatusFrom(resp.Common), NodeTypes: []NodeType{}, Types: []DataType{}, Kinds: []Kind{}, Conversions: []Conversion{}}
	for _, t := range resp.NodeTypes {
		pins := pinsOf(t.Pins)
		out.NodeTypes = append(out.NodeTypes, NodeType{ID: t.ID, Title: t.Title, Category: t.Category, Description: t.Description, Color: t.Color,
			Source: t.Source, Event: t.Event, Config: t.Config, Pins: pins, PinsFromConfig: t.PinsFor != nil, Example: t.Example, Kinds: t.Kinds, Effects: t.Effects})
	}
	for _, t := range resp.Types {
		out.Types = append(out.Types, DataType{ID: t.ID, Color: t.Color, Description: t.Description})
	}
	for _, k := range resp.Kinds {
		out.Kinds = append(out.Kinds, Kind{ID: k.ID, Title: k.Title, Description: k.Description, Returns: k.Returns, Events: k.Events, Pure: k.Pure, Spec: k.Spec})
	}
	for _, c := range resp.Conversions {
		out.Conversions = append(out.Conversions, Conversion{From: c.From, To: c.To})
	}
	return out
}
