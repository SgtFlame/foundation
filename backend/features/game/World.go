package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Events of a world.
const (
	EventCreated  = "created"  // {world}
	EventChanged  = "changed"  // {name?, generator?}
	EventDeleted  = "deleted"  // {}
	EventRestored = "restored" // {}
)

// MaxName is the longest name a world may have, in characters.
const MaxName = 80

// World is a world definition as the projection keeps it.
type World struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Generator Moon      `json:"generator"`
	Deleted   bool      `json:"deleted,omitempty"`
	Version   uint64    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const worldFields = `{id, name, generator: {seed, voxelSize, radius, amplitude, frequency, octaves, craters, craterMaxRadius, dustDepth}, deleted, version, createdAt, updatedAt}`

type worldChange struct {
	World     *World `json:"world,omitempty"`
	Name      string `json:"name,omitempty"`
	Generator *Moon  `json:"generator,omitempty"`
}

func foldWorld(f live.Fold) (json.RawMessage, error) {
	var w World
	if f.State != nil {
		if err := json.Unmarshal(f.State, &w); err != nil {
			return nil, err
		}
	}
	var c worldChange
	if err := json.Unmarshal(f.Event.Data, &c); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventCreated:
		if c.World == nil {
			return f.State, nil
		}
		w = *c.World
		w.CreatedAt = f.Event.Time
	case EventChanged:
		if c.Name != "" {
			w.Name = c.Name
		}
		if c.Generator != nil {
			w.Generator = *c.Generator
		}
	case EventDeleted:
		w.Deleted = true
	case EventRestored:
		w.Deleted = false
	default:
		return f.State, nil
	}
	w.Version++
	w.UpdatedAt = f.Event.Time
	return json.Marshal(w)
}

func problem(code, path, message, fix string) []live.Problem {
	return []live.Problem{{Code: code, Path: path, Message: message, Fix: fix}}
}

func checkName(name string) (string, []live.Problem) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", problem("", "args.name", "a world needs a name", "")
	case utf8.RuneCountInString(name) > MaxName:
		return "", problem("", "args.name", fmt.Sprintf("a world's name is at most %d characters", MaxName), "")
	}
	return name, nil
}

// generatorFrom is base with the fields raw sets.
func generatorFrom(base Moon, raw json.RawMessage) (Moon, []live.Problem) {
	g := base
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &g); err != nil {
			return g, problem("", "args.generator", err.Error(), "")
		}
	}
	if path, msg := g.Check("args.generator"); path != "" {
		return g, problem("bad_generator", path, msg, "")
	}
	return g, nil
}

// NewWorldID is a new world's id.
func NewWorldID() string { return "w" + strconv.FormatUint(features.NewTSID(), 10) }

func contributeWorlds(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: WorldsProjector, Source: source}, Title: "Game worlds",
		Description: "Each game world definition, keyed gameWorld.<id>: " + worldFields,
		Area:        live.AreaDefinitions, Types: []string{WorldType}, Fold: foldWorld,
	})
	register := func(c live.Command, decide func(d live.Decision, w World, exists bool) ([]live.NewEvent, []live.Problem)) {
		c.Source = source
		c.Area, c.TargetType, c.Projection = live.AreaDefinitions, WorldType, WorldsProjector
		c.Action = "game.write"
		c.Resource = func(p features.Principal, _ live.Target) string { return Resource(p) }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var w World
			exists := d.State != nil
			if exists {
				if err := json.Unmarshal(d.State, &w); err != nil {
					return nil, problem("", "", "the world cannot be read: "+err.Error(), "")
				}
			}
			return decide(d, w, exists)
		}
		live.Commands.Register(reg, c)
	}
	missing := func(d live.Decision) []live.Problem {
		return problem("world_not_found", "target", fmt.Sprintf("there is no world %q", d.Target.ID), "list the worlds with the game.worlds query")
	}

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.createWorld"}, Title: "Make a world",
		Description: "Makes a game world: a moon made by a seeded generator, which players fly over and dig. Generator fields left out take the defaults (seed 1, a 100 m moon of 0.5 m voxels).",
		Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"The world's name, at most 80 characters, e.g. Test moon"},
  "generator":` + moonSchema + `},"required":["name"]}`),
		Examples:  []json.RawMessage{json.RawMessage(`{"name":"Test moon","generator":{"seed":7}}`)},
		Events:    []string{EventCreated},
		NewTarget: NewWorldID,
	}, func(d live.Decision, _ World, exists bool) ([]live.NewEvent, []live.Problem) {
		if exists {
			return nil, problem("world_exists", "target", fmt.Sprintf("the world %q exists", d.Target.ID), "change it with game.setWorld")
		}
		var a struct {
			Name      string          `json:"name"`
			Generator json.RawMessage `json:"generator"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		name, ps := checkName(a.Name)
		if ps != nil {
			return nil, ps
		}
		g, ps := generatorFrom(DefaultMoon, a.Generator)
		if ps != nil {
			return nil, ps
		}
		d.Undo("", live.OpOf("game.deleteWorld", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventCreated, Data: worldChange{World: &World{ID: d.Target.ID, Name: name, Generator: g}}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.setWorld"}, Title: "Change a world",
		Description: "Renames a world or changes its generator: fields of generator left out keep their value. A new seed regenerates the moon for everyone viewing it; its terrain edits stay where they were.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"The world's new name"},
  "generator":` + moonSchema + `}}`),
		Examples: []json.RawMessage{json.RawMessage(`{"generator":{"seed":42}}`), json.RawMessage(`{"name":"Crater field","generator":{"craters":80}}`)},
		Events:   []string{EventChanged},
	}, func(d live.Decision, w World, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists || w.Deleted {
			return nil, missing(d)
		}
		var a struct {
			Name      *string         `json:"name"`
			Generator json.RawMessage `json:"generator"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		c := worldChange{}
		if a.Name != nil {
			name, ps := checkName(*a.Name)
			if ps != nil {
				return nil, ps
			}
			if name != w.Name {
				c.Name = name
			}
		}
		if len(a.Generator) > 0 {
			g, ps := generatorFrom(w.Generator, a.Generator)
			if ps != nil {
				return nil, ps
			}
			if g != w.Generator {
				c.Generator = &g
			}
		}
		if c.Name == "" && c.Generator == nil {
			d.Undo("") // nothing to change, nothing to undo
			return nil, nil
		}
		undo := map[string]any{}
		if c.Name != "" {
			undo["name"] = w.Name
		}
		if c.Generator != nil {
			// Only the fields it changes, so undoing leaves others' changes
			undo["generator"] = changedFields(w.Generator, *c.Generator)
		}
		d.Undo("", live.OpOf("game.setWorld", d.Target.ID, undo))
		return []live.NewEvent{{Type: EventChanged, Data: c}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.deleteWorld"}, Title: "Delete a world",
		Description: "Deletes a world; game.restoreWorld brings it back with its terrain edits.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventDeleted},
	}, func(d live.Decision, w World, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		if w.Deleted {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("game.restoreWorld", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventDeleted, Data: worldChange{}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.restoreWorld"}, Title: "Restore a world",
		Description: "Brings back a deleted world.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventRestored},
	}, func(d live.Decision, w World, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		if !w.Deleted {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("game.deleteWorld", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventRestored, Data: worldChange{}}}, nil
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.worlds", Source: source}, Title: "Game worlds",
		Description: "Every game world that is not deleted (with deleted: true, the deleted ones too), by key gameWorld.<id>: " + worldFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean","description":"Also list the deleted worlds, which game.restoreWorld brings back"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Run:         liveWorlds,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.world", Source: source}, Title: "A game world",
		Description: "One game world's definition, or null when there is none: " + worldFields,
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The world's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"w123"}`)},
		Action:      "game.read",
		Run: live.ValueOf(WorldsProjector, func(f live.Feed) (string, error) {
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.ID) {
				return "", fmt.Errorf("game.world needs a world's id")
			}
			return WorldType + "." + p.ID, nil
		}, json.RawMessage(`null`)),
	})
}

// liveWorlds is game.worlds: the worlds that are not deleted (or all), as a map by
// key, then upserts and removals.
func liveWorlds(f live.Feed) error {
	var p struct {
		Deleted bool `json:"deleted"`
	}
	_ = json.Unmarshal(f.Params(), &p)
	entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, WorldsProjector, WorldType+".*")
	if err != nil {
		return err
	}
	out := map[string]json.RawMessage{}
	pos := live.Position{}
	started := false
	for e := range entries {
		if e == nil {
			started = true
			f.Snapshot(out, pos)
			continue
		}
		pos = pos.Merge(e.Value.Position())
		var w World
		gone := e.Deleted || json.Unmarshal(e.Value.Value, &w) != nil || (w.Deleted && !p.Deleted)
		if gone {
			if _, had := out[e.Key]; had {
				delete(out, e.Key)
				if started {
					f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
				}
			}
			continue
		}
		out[e.Key] = e.Value.Value
		if started {
			f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: e.Value.Value}, pos)
		}
	}
	return nil
}

// changedFields is old's value of each field that differs in new.
func changedFields(old, new Moon) map[string]any {
	var a, b map[string]any
	ra, _ := json.Marshal(old)
	rb, _ := json.Marshal(new)
	_ = json.Unmarshal(ra, &a)
	_ = json.Unmarshal(rb, &b)
	out := map[string]any{}
	for k, v := range a {
		if b[k] != v {
			out[k] = v
		}
	}
	return out
}
