package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

func areaQueries(reg *extensions.Registry) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.areas", Source: source}, Title: "Game areas",
		Description: "Every game area that is not deleted (with deleted: true, the deleted ones too), by key gameArea.<id>: " + areaFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean","description":"Also list the deleted areas, which game.restoreArea brings back"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Run:         func(f live.Feed) error { return liveDefs(f, AreasProjector, AreaType) },
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.area", Source: source}, Title: "A game area",
		Description: "One game area's definition with its placed pieces, or null when there is none: " + areaFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The area's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"a123"}`)},
		Action:      "game.read",
		Run: live.ValueOf(AreasProjector, func(f live.Feed) (string, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.ID) {
				return "", fmt.Errorf("game.area needs an area's id")
			}
			return AreaType + "." + p.ID, nil
		}, json.RawMessage(`null`)),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.pieceSets", Source: source}, Title: "Piece sets",
		Description: "The piece sets areas are laid out from, by id: {id, title, description, cell, edgeTypes, meets, pieces: [{id, title, description, size: [width, depth] in cells, model, edges: {n, e, s, w}}]}. Built in for now; they do not change while the server runs.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Run: func(f live.Feed) error {
			out := map[string]*PieceSet{}
			for _, id := range PieceSetIDs() {
				out[id], _ = PieceSetOf(id)
			}
			f.Snapshot(out, live.Position{})
			<-f.Context().Done()
			return nil
		},
	})
}
