package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Wrecks (forge 360 V15; the play design's scavenging): two derelict skiffs
// half buried in the moon, k0 and k1, placed by the engine from the world's
// seed (wreck/Wreck.ts wreckSites). game.salvage (cmd-game-salvage) tears
// one block off a wreck for scrap, which goes into the sender's pack as
// ore (salvage and mining feed the same chain); a block salvaged is gone
// for everyone. game.wrecks keeps, per world, which blocks are gone.

const (
	// WrecksProjector keeps each world's salvaged wreck blocks, keyed terrain.<worldId>.
	WrecksProjector = "game.wrecks"
	// EventSalvaged is a wreck's block torn off: {wreck, index, pack: {ore}}.
	EventSalvaged = "salvaged"
	// SalvageOre is the scrap one block gives, in m³ of ore.
	SalvageOre = 0.25
)

// Wrecks is what a world's players salvaged.
type Wrecks struct {
	Salvaged map[string]bool `json:"salvaged"` // <wreck>.<block index>
	Count    int             `json:"count"`
}

func foldWrecks(f live.Fold) (json.RawMessage, error) {
	w := Wrecks{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &w); err != nil {
			return nil, err
		}
	}
	if w.Salvaged == nil {
		w.Salvaged = map[string]bool{}
	}
	if f.Event.Type != EventSalvaged {
		// stored with every event on the target, so a command's expected sequence is the target's
		return json.Marshal(w)
	}
	var d struct {
		Wreck string `json:"wreck"`
		Index int    `json:"index"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	k := fmt.Sprintf("%s.%d", d.Wreck, d.Index)
	if !w.Salvaged[k] {
		w.Salvaged[k] = true
		w.Count++
	}
	return json.Marshal(w)
}

func contributeWrecks(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: WrecksProjector, Source: source}, Title: "Wrecks salvaged",
		Description: "Which blocks of each world's two moon wrecks (k0, k1) were salvaged, keyed terrain.<worldId>: {salvaged: {<wreck>.<index>: true}, count}",
		Area:        Area, Types: []string{TerrainType},
		Fold: foldWrecks,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.wrecks", Source: source}, Title: "Wrecks",
		Description: fmt.Sprintf("A world's wrecks salvaged: {salvaged: {<wreck>.<index>: true}, count}, then each change. Each block salvaged with game.salvage gave %g m³ of ore and is gone for everyone.", SalvageOre),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(WrecksProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.wrecks needs a world's id")
			}
			return TerrainType + "." + p.World, nil
		}, json.RawMessage(`{"salvaged":{},"count":0}`)),
	})
}
