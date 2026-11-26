package game

import (
	"embed"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The game's built-in graphs: the terrain edit command (V1.5), editable,
// so a tenant can change the rules by publishing its own version.
//
//go:embed graphs/*.json
var graphFiles embed.FS

func contributeGraphs(reg *extensions.Registry) {
	entries, err := graphFiles.ReadDir("graphs")
	if err != nil {
		panic(err)
	}
	for i, e := range entries {
		raw, err := graphFiles.ReadFile("graphs/" + e.Name())
		if err != nil {
			panic(err)
		}
		var g cognition.Graph
		if err := json.Unmarshal(raw, &g); err != nil {
			panic("game: graphs/" + e.Name() + ": " + err.Error())
		}
		cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: g.ID, Order: 600 + i, Source: source}, Graph: g, Editable: true})
	}
}
