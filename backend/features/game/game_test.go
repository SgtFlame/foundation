package game_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/game"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

type stack struct {
	t   *testing.T
	bus *bus.Bus
	p   *project.Projections
	reg *extensions.Registry
	n   int
}

func newStack(t *testing.T) *stack {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	var draftDeps drafts.Deps
	var liveDeps livenodes.Deps
	var gameDeps game.Deps
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	livenodes.Contribute(reg, func() livenodes.Deps { return liveDeps })
	game.ContributeAccess(reg)
	game.Contribute(reg, func() game.Deps { return gameDeps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, cognition.Models()...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	pres := presence.New(srv.JS(), nil)
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	liveDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p, Presence: pres}
	gameDeps = game.Deps{Store: p}
	b.Resolve = livenodes.CommandFor(func() livenodes.Deps { return liveDeps })
	return &stack{t: t, bus: b, p: p, reg: reg}
}

func userArgs(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
}

func (st *stack) send(user uint64, typ, target string, args any) bus.Result {
	st.t.Helper()
	st.n++
	raw, _ := json.Marshal(args)
	return st.bus.Execute(bus.Request{ServiceArgs: userArgs(user), ID: fmt.Sprintf("t%d", st.n), Type: typ, Target: target, Args: raw})
}

func (st *stack) ok(typ, target string, args any) bus.Result {
	st.t.Helper()
	res := st.send(1, typ, target, args)
	if res.Common.Code >= 300 {
		st.t.Fatalf("%s %s: %s %+v", typ, target, res.Common.Message, res.Problems)
	}
	return res
}

func (st *stack) refused(typ, target string, args any) bus.Result {
	st.t.Helper()
	res := st.send(1, typ, target, args)
	if res.Common.Code < 300 {
		st.t.Fatalf("%s %s: committed, expected a refusal", typ, target)
	}
	return res
}

func (st *stack) read(projector, key string, seq uint64, into any) bool {
	st.t.Helper()
	v, err := st.p.WaitFor(context.Background(), 1, projector, key, seq, 3*time.Second)
	if err != nil {
		st.t.Fatal(err)
	}
	if v.Value == nil {
		return false
	}
	if err := json.Unmarshal(v.Value, into); err != nil {
		st.t.Fatal(err)
	}
	return true
}

func TestWorldOps(t *testing.T) {
	st := newStack(t)
	res := st.ok("game.createWorld", "", map[string]any{"name": "Test moon", "generator": map[string]any{"seed": 7}})
	id := res.Target
	var w game.World
	if !st.read(game.WorldsProjector, game.WorldType+"."+id, res.Seq, &w) {
		t.Fatal("no world")
	}
	if w.Name != "Test moon" || w.Generator.Seed != 7 || w.Generator.Radius != game.DefaultMoon.Radius {
		t.Fatalf("world %+v", w)
	}
	res = st.ok("game.setWorld", id, map[string]any{"generator": map[string]any{"seed": 42, "craters": 80}})
	st.read(game.WorldsProjector, game.WorldType+"."+id, res.Seq, &w)
	if w.Generator.Seed != 42 || w.Generator.Craters != 80 || w.Generator.VoxelSize != 0.5 {
		t.Fatalf("after setWorld %+v", w.Generator)
	}
	st.refused("game.setWorld", id, map[string]any{"generator": map[string]any{"radius": 5}})
	st.refused("game.setWorld", "nope", map[string]any{"name": "x"})
	res = st.ok("game.deleteWorld", id, map[string]any{})
	st.read(game.WorldsProjector, game.WorldType+"."+id, res.Seq, &w)
	if !w.Deleted {
		t.Fatal("not deleted")
	}
	st.refused("game.editTerrain", id, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 0, "radius": 2})
	res = st.ok("game.restoreWorld", id, map[string]any{})
	w = game.World{}
	st.read(game.WorldsProjector, game.WorldType+"."+id, res.Seq, &w)
	if w.Deleted {
		t.Fatal("not restored")
	}
}

func TestTerrainEdits(t *testing.T) {
	st := newStack(t)
	id := st.ok("game.createWorld", "", map[string]any{"name": "Dig site"}).Target
	a := st.ok("game.editTerrain", id, map[string]any{"kind": "dig", "x": 1.5, "y": 99, "z": -2, "radius": 2})
	b := st.ok("game.editTerrain", id, map[string]any{"kind": "fill", "x": 3, "y": 99.5, "z": -1, "radius": 1.25})
	// One far enough to land in another cell
	c := st.ok("game.editTerrain", id, map[string]any{"kind": "dig", "x": -40, "y": 90, "z": 5, "radius": 3})
	for _, bad := range []map[string]any{
		{"kind": "dig", "x": 0, "y": 99, "z": 0, "radius": 9},
		{"kind": "dig", "x": 0, "y": 99, "z": 0, "radius": 0.1},
		{"kind": "blast", "x": 0, "y": 99, "z": 0, "radius": 2},
		{"kind": "dig", "x": 0, "y": 900, "z": 0, "radius": 2},
	} {
		st.refused("game.editTerrain", id, bad)
	}
	st.refused("game.editTerrain", "w404", map[string]any{"kind": "dig", "x": 0, "y": 99, "z": 0, "radius": 2})

	var cell game.Cell
	if !st.read(game.CellsProjector, game.CellKey(id, 1.5, 99, -2), b.Seq, &cell) {
		t.Fatal("no cell")
	}
	if len(cell.Edits) != 2 || cell.Edits[0].Seq != a.Seq || cell.Edits[1].Seq != b.Seq || cell.Edits[1].Kind != "fill" || cell.Edits[0].By != 1 {
		t.Fatalf("cell %+v (seqs %d %d)", cell, a.Seq, b.Seq)
	}
	if cell.Edits[1].Material == nil || *cell.Edits[1].Material != 0 || cell.Edits[0].Material != nil {
		t.Fatalf("materials %+v", cell.Edits)
	}
	var terrain game.Terrain
	st.read(game.TerrainProjector, game.TerrainType+"."+id, c.Seq, &terrain)
	if terrain.Edits != 3 || terrain.LastSeq != c.Seq {
		t.Fatalf("terrain %+v", terrain)
	}
	var other game.Cell
	if !st.read(game.CellsProjector, game.CellKey(id, -40, 90, 5), c.Seq, &other) || len(other.Edits) != 1 {
		t.Fatalf("other cell %+v", other)
	}

	// The model's description
	var tool *tools.Tool
	for _, x := range tools.Tools.All(st.reg) {
		if x.ID == "game.describeWorld" {
			tool = &x
		}
	}
	if tool == nil {
		t.Fatal("no game.describeWorld")
	}
	r, err := tool.Run(tools.Call{ServiceArgs: userArgs(1), Name: tool.ID, Args: json.RawMessage(`{"world":"` + id + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	edits := r.Content["edits"].(map[string]any)
	if edits["total"].(float64) != 3 || edits["digs"].(float64) != 2 || edits["cells"].(float64) != 2 {
		t.Fatalf("description %v", r.Content)
	}
	t.Log(r.Summary)
}
