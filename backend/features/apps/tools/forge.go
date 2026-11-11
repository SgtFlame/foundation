package apptools

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// ForgeDeps are what the forge.* tools read, made after the registry.
type ForgeDeps struct {
	Subscriptions *subscribe.Subscriptions
	Store         ops.Store // the projections, for an app's problems
	Bus           *bus.Bus  // forge.apply's and forge.undo's commands (forge 06e)
	// Engine runs the graphs forge.test tests (forge 06f).
	Engine *engine.Engine
	// Presence shows a chat round's Builder where forge.apply works (forge
	// 06g); nil shows nothing.
	Presence *presence.Presence
	// Connectors is the connector registry (forge 07b); without it the
	// connectors area lists the builtin contracts and types, offline.
	Connectors *connectors.Registry
}

// Catalog areas (forge 06d): what forge.catalog searches.
const (
	AreaComponents = "components"
	AreaNodes      = "nodes"
	AreaFields     = "fields"
	AreaPrefabs    = "prefabs"
	AreaConnectors = "connectors" // contracts and connector types (forge 07b)
)

// Areas are the catalog's areas, in the order forge.catalog lists them.
var Areas = []string{AreaComponents, AreaNodes, AreaFields, AreaPrefabs, AreaConnectors}

// Item is one catalog entry as forge.catalog lists it; forge.describe
// gives the whole of it by its ref.
type Item struct {
	Ref         string   `json:"ref"` // component:<id>, node:<type>, field:<type>, prefab:<id>, contract:<id@v>, connector:<type>
	Area        string   `json:"area"`
	Title       string   `json:"title"`
	Category    string   `json:"category,omitempty"`
	Description string   `json:"description"` // its first sentence
	Tags        []string `json:"tags"`
	score       int
}

// short is a description's first sentence.
func short(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if len(s) > 200 {
		s = s[:197] + "..."
	}
	return s
}

// matches reports whether an item holds every word of a search, and
// scores it: two for each word in its title, one for each in its tags.
func (it *Item) matches(words []string) bool {
	if len(words) == 0 {
		return true
	}
	hay := strings.ToLower(it.Ref + " " + it.Title + " " + it.Category + " " + it.Description + " " + strings.Join(it.Tags, " "))
	title, tags := strings.ToLower(it.Title), strings.ToLower(strings.Join(it.Tags, " "))
	it.score = 0
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
		if strings.Contains(title, w) {
			it.score += 2
		}
		if strings.Contains(tags, w) {
			it.score++
		}
	}
	return true
}

// ContributeForge registers the forge.* read tools (forge 06d): the
// catalog of what an app is built with, one catalog item in full, an app
// with its problems and who has it open, and one part in full. They are
// views over the registries and queries the other read tools use, beside
// them for every chat (06d question 1).
func ContributeForge(reg *extensions.Registry, deps func() ForgeDeps) {
	once := func(call tools.Call, query string, params any) (any, live.Position, error) {
		raw, _ := json.Marshal(params)
		data, pos, fail := deps().Subscriptions.Once(call.ServiceArgs, query, raw)
		if fail != nil {
			return nil, nil, fmt.Errorf("%s", fail.Message)
		}
		return data, pos, nil
	}
	// prefabOf reads a prefab's last published version, else its draft,
	// and the version it is (0 for a draft never published)
	prefabOf := func(call tools.Call, id string) (ui.Prefab, int, error) {
		data, _, err := once(call, "prefab.describe", map[string]any{"id": id, "published": true})
		if err != nil {
			if data, _, err = once(call, "prefab.describe", map[string]any{"id": id}); err != nil {
				return ui.Prefab{}, 0, err
			}
		}
		raw, _ := json.Marshal(data)
		var v struct {
			Prefab    ui.Prefab
			Published int
		}
		_ = json.Unmarshal(raw, &v)
		version := v.Prefab.Version
		if version == 0 {
			version = v.Published
		}
		return v.Prefab, version, nil
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.catalog", Source: "apps"}, Title: "Search the catalog", Effect: tools.EffectRead,
		Description: "Searches what applications are built with: UI primitives (components), Blueprint node types (nodes), entity field types (fields), the tenant's published prefabs, the starter library's included (prefabs), and connectors: contracts (contract:<id@version>, APIs of services outside the tenant that Call connector calls) and connector types (connector:<type>, their implementations). Each item has a ref, title, category, its description's first sentence and tags; forge.describe {ref} gives one in full, with its schema and examples. area narrows it to one area; search keeps items holding every word, best matches first.",
		Input:       json.RawMessage(`{"type":"object","properties":{"area":{"type":"string","enum":["components","nodes","fields","prefabs","connectors"],"description":"One area; every area when left out"},"search":{"type":"string","description":"Words every item found holds, in its title, description, category or tags, e.g. table"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Area, Search string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			if a.Area != "" && !slices.Contains(Areas, a.Area) {
				return tools.Result{}, fmt.Errorf("there is no area %q: use one of %s", a.Area, strings.Join(Areas, ", "))
			}
			words := strings.Fields(strings.ToLower(a.Search))
			var items []Item
			add := func(it Item) {
				if it.Tags == nil {
					it.Tags = []string{}
				}
				if it.matches(words) {
					items = append(items, it)
				}
			}
			in := func(area string) bool { return a.Area == "" || a.Area == area }
			if in(AreaComponents) {
				for _, c := range ui.Components.All(reg) {
					add(Item{Ref: "component:" + c.ID, Area: AreaComponents, Title: c.Title, Category: c.Category, Description: short(c.Description), Tags: c.Tags})
				}
			}
			if in(AreaNodes) {
				for _, t := range cognition.Nodes.All(reg) {
					add(Item{Ref: "node:" + t.ID, Area: AreaNodes, Title: t.Title, Category: t.Category, Description: short(t.Description), Tags: cognition.TagsOf(t)})
				}
			}
			if in(AreaFields) {
				for _, f := range records.FieldTypes {
					add(Item{Ref: "field:" + f.ID, Area: AreaFields, Title: f.Title, Category: f.Group, Description: short(f.Description), Tags: f.Tags})
				}
			}
			if in(AreaPrefabs) && deps().Subscriptions != nil {
				data, _, err := once(call, "prefab.list", map[string]any{})
				if err != nil {
					return tools.Result{}, err
				}
				raw, _ := json.Marshal(data)
				var byKey map[string]prefabRow
				_ = json.Unmarshal(raw, &byKey)
				for _, p := range byKey {
					if p.Published > 0 && !p.Deleted {
						tags := slices.Clone(p.Tags)
						if p.Owner != "" {
							tags = append(tags, p.Owner)
						}
						add(Item{Ref: "prefab:" + p.ID, Area: AreaPrefabs, Title: p.Name, Category: "Prefabs", Description: short(p.Description), Tags: tags})
					}
				}
			}
			if in(AreaConnectors) {
				cat := catalogOf(call, reg, deps().Connectors)
				for _, c := range cat.Contracts {
					tags := append(slices.Clone(c.Contract.Meta.Tags), "contract")
					if !c.Online {
						tags = append(tags, "offline")
					}
					add(Item{Ref: "contract:" + c.Ref, Area: AreaConnectors, Title: c.Contract.Meta.Title, Category: "Contracts", Description: short(c.Contract.Meta.Description), Tags: tags})
				}
				for _, t := range cat.Types {
					tags := append(slices.Clone(t.Type.Meta.Tags), "connector", t.Source)
					if !t.Online {
						tags = append(tags, "offline")
					}
					add(Item{Ref: "connector:" + t.Type.ID, Area: AreaConnectors, Title: t.Type.Meta.Title, Category: "Connector types", Description: short(t.Type.Meta.Description), Tags: tags})
				}
			}
			order := func(area string) int { return slices.Index(Areas, area) }
			slices.SortFunc(items, func(x, y Item) int {
				if x.score != y.score {
					return y.score - x.score
				}
				if x.Area != y.Area {
					return order(x.Area) - order(y.Area)
				}
				return strings.Compare(x.Ref, y.Ref)
			})
			if items == nil {
				items = []Item{}
			}
			return tools.Result{Content: map[string]any{"items": items}, Summary: fmt.Sprintf("%d catalog items", len(items))}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.describe", Source: "apps"}, Title: "Describe a catalog item", Effect: tools.EffectRead,
		Description: "Gives one catalog item in full by its ref (from forge.catalog): a component's props schema, events, slots and example trees; a node type's pins, config schema, kinds and examples ({type, config}); a field type's group and example fields (as entity.addField takes them); a prefab's interface as a component (props schema, events, slots, variants) with example nodes from its preview data, from its last published version; a contract's operations (args and result schemas, examples, effect) and events, with the types implementing it, to call with Call connector {contract, operation}; a connector type's contracts and settings. Each has its title, description and tags.",
		Input:       json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string","description":"component:<id>, node:<type>, field:<type>, prefab:<id>, contract:<id@version> or connector:<type>, e.g. prefab:recordTable"}},"required":["ref"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Ref string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			kind, id, _ := strings.Cut(a.Ref, ":")
			// A prefab named as a component is the prefab
			if p, ok := strings.CutPrefix(id, ui.PrefabPrefix); ok && kind == "component" {
				kind, id = "prefab", p
			}
			out := map[string]any{"ref": a.Ref}
			switch kind {
			case "component":
				c, ok := ui.Catalog(reg)[id]
				if !ok {
					return tools.Result{}, fmt.Errorf("there is no component %q; search them with forge.catalog {area: \"components\"}", id)
				}
				maps.Copy(out, map[string]any{"area": AreaComponents, "id": c.ID, "title": c.Title, "description": c.Description, "category": c.Category, "tags": c.Tags,
					"props": c.Props, "events": c.Events, "slots": c.Slots, "children": c.Children, "examples": c.Examples})
				return tools.Result{Content: out, Summary: c.Title}, nil
			case "node":
				t, ok := cognition.CatalogOf(reg).Type(id)
				if !ok {
					return tools.Result{}, fmt.Errorf("there is no node type %q; search them with forge.catalog {area: \"nodes\"}", id)
				}
				pins := t.Pins
				if t.PinsFor != nil {
					if p, err := t.PinsFor(nil); err == nil {
						pins = p
					}
				}
				maps.Copy(out, map[string]any{"area": AreaNodes, "id": t.ID, "title": t.Title, "description": t.Description, "category": t.Category, "tags": cognition.TagsOf(t),
					"example": t.Example, "examples": cognition.ExamplesOf(t), "pins": pins, "pinsFromConfig": t.PinsFor != nil, "config": t.Config,
					"kinds": t.Kinds, "event": t.Event, "effects": t.Effects})
				return tools.Result{Content: out, Summary: t.Title}, nil
			case "field":
				f, ok := records.FieldTypeOf(id)
				if !ok {
					return tools.Result{}, fmt.Errorf("there is no field type %q; search them with forge.catalog {area: \"fields\"}", id)
				}
				maps.Copy(out, map[string]any{"area": AreaFields, "id": f.ID, "title": f.Title, "description": f.Description, "category": f.Group, "tags": f.Tags,
					"computed": f.Computed, "examples": f.Examples})
				return tools.Result{Content: out, Summary: f.Title}, nil
			case "prefab":
				p, version, err := prefabOf(call, id)
				if err != nil {
					return tools.Result{}, err
				}
				c := ui.PrefabComponent(p)
				variants := map[string]string{}
				for name, v := range p.Variants {
					variants[name] = v.Description
				}
				maps.Copy(out, map[string]any{"area": AreaPrefabs, "id": p.ID, "type": c.ID, "title": c.Title, "description": c.Description, "category": c.Category, "tags": c.Tags,
					"owner": p.Owner, "version": version, "props": c.Props, "events": c.Events, "slots": c.Slots, "variants": variants, "examples": c.Examples})
				return tools.Result{Content: out, Summary: c.Title}, nil
			case "contract", "connector":
				d, err := describeConnector(call, reg, deps().Connectors, a.Ref)
				if err != nil {
					return tools.Result{}, err
				}
				maps.Copy(out, d)
				out["area"] = AreaConnectors
				return tools.Result{Content: out, Summary: a.Ref}, nil
			}
			return tools.Result{}, fmt.Errorf("%q is not a catalog ref: use component:<id>, node:<type>, field:<type>, prefab:<id>, contract:<id@version> or connector:<type>, as forge.catalog gives them", a.Ref)
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.app", Source: "apps"}, Title: "Read an app", Effect: tools.EffectRead,
		Description: "Reads an application's draft: its manifest (pages with their titles and descriptions, nav, editor kinds, entity types, commands, queries, roles, settings, triggers), its rev, and a summary of each part (the prefabs, graphs and entity types it names or owns: name, description, kind, rev, published version, missing); its current problems, each at a path with a fix; who else has it open; the position (stream → sequence) it was read at, and after, the sequence of the app's last op, to pass to forge.apply and forge.changes. Read a part in full with forge.read.",
		Input:       json.RawMessage(`{"type":"object","properties":{"appId":{"type":"string","description":"The app's id (app.list)"}},"required":["appId"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ AppID string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			data, pos, err := once(call, "app.describe", map[string]any{"id": a.AppID})
			if err != nil {
				return tools.Result{}, err
			}
			raw, _ := json.Marshal(data)
			var v manifests.View
			_ = json.Unmarshal(raw, &v)
			problems := []live.Problem{}
			if store := deps().Store; store != nil {
				env := manifests.Env(call.ServiceArgs.Context(), store, call.ServiceArgs.Principal.TenantID, reg)
				problems = append(problems, apps.CheckManifest(v.Manifest, env)...)
			}
			// Who else: the other users present in the app
			here := []map[string]any{}
			if entries, _, err := once(call, presence.QueryID, map[string]any{"object": "app." + a.AppID}); err == nil {
				raw, _ := json.Marshal(entries)
				var list []presence.Entry
				_ = json.Unmarshal(raw, &list)
				seen := map[uint64]bool{call.ServiceArgs.Principal.UserID: true}
				for _, e := range list {
					if !seen[e.UserID] {
						seen[e.UserID] = true
						here = append(here, map[string]any{"userId": fmt.Sprint(e.UserID), "name": e.Name})
					}
				}
			}
			content := map[string]any{"app": v, "problems": problems, "here": here, "position": pos}
			// The app's last op, for forge.apply's and forge.changes' after
			// (forge 06e): the position holds only what the query read
			if store := deps().Store; store != nil {
				if entries, err := ops.Entries(call.ServiceArgs, store, a.AppID); err == nil {
					var after uint64
					if len(entries) > 0 {
						after = entries[len(entries)-1].Seq
					}
					content["after"] = after
				}
			}
			summary := fmt.Sprintf("Read the app %s: %d parts, %d problems", v.Manifest.Name, len(v.Parts), len(problems))
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.read", Source: "apps"}, Title: "Read a part", Effect: tools.EffectRead,
		Description: "Reads one part of an app in full, as its draft is now: prefab:<id> (its interface, variants, template with key paths, graphs and preview data), graph:<id> (its nodes with their config, edges, variables, each node's pins and its problems) or entity:<id> (its fields, title field, indexes and row rules); with its rev, the published version, and the position (stream → sequence) it was read at. forge.app lists an app's parts.",
		Input:       json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string","description":"prefab:<id>, graph:<id> or entity:<id>, e.g. graph:cmd-tasks-task-reassign"}},"required":["ref"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Ref string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			kind, id, _ := strings.Cut(a.Ref, ":")
			query, ok := map[string]string{"prefab": "prefab.describe", "graph": "cognition.graph", "entity": "entity.describe"}[kind]
			if !ok || id == "" {
				return tools.Result{}, fmt.Errorf("%q is not a part: use prefab:<id>, graph:<id> or entity:<id>, as forge.app's parts name them", a.Ref)
			}
			data, pos, err := once(call, query, map[string]any{"id": id})
			if err != nil {
				return tools.Result{}, err
			}
			return tools.Result{Content: map[string]any{"ref": a.Ref, "part": data, "position": pos}, Summary: "Read " + a.Ref}, nil
		},
	})
}

// prefabRow is one prefab of the prefab.list query.
type prefabRow struct {
	ID, Name, Description, Owner string
	Tags                         []string
	Published                    int
	Deleted                      bool
}

// catalogOf is the connector registry's catalog for the caller's tenant,
// or, with no registry, the builtin contracts and types, offline.
func catalogOf(call tools.Call, reg *extensions.Registry, r *connectors.Registry) connectors.Catalog {
	if r != nil {
		if cat, err := r.Catalog(call.ServiceArgs.Context(), call.ServiceArgs.Principal.TenantID); err == nil {
			return cat
		}
	}
	var cat connectors.Catalog
	types := map[string][]string{}
	for _, t := range connectors.Types.All(reg) {
		cat.Types = append(cat.Types, connectors.TypeInfo{Type: t.Type, Source: connectors.FromBuiltin, Direction: connectors.Outbound})
		for _, ref := range t.Type.Contracts {
			types[ref] = append(types[ref], t.Type.ID)
		}
	}
	for _, c := range connectors.Contracts.All(reg) {
		cat.Contracts = append(cat.Contracts, connectors.ContractInfo{Contract: c.Contract, Ref: c.Contract.Ref(), Source: connectors.FromBuiltin, Types: types[c.Contract.Ref()]})
	}
	return cat
}

// describeConnector is forge.describe of a contract:<ref> or
// connector:<type>.
func describeConnector(call tools.Call, reg *extensions.Registry, r *connectors.Registry, ref string) (map[string]any, error) {
	if r != nil {
		return r.Describe(call.ServiceArgs.Context(), call.ServiceArgs.Principal.TenantID, ref)
	}
	kind, id, _ := strings.Cut(ref, ":")
	cat := catalogOf(call, reg, nil)
	if kind == "contract" {
		for _, c := range cat.Contracts {
			if c.Ref == id || c.Contract.ID == id {
				return map[string]any{"ref": ref, "kind": "contract", "contract": c.Contract, "source": c.Source, "online": false, "types": c.Types}, nil
			}
		}
		return nil, fmt.Errorf("there is no contract %q; search them with forge.catalog {area: \"connectors\"}", id)
	}
	for _, t := range cat.Types {
		if t.Type.ID == id {
			return map[string]any{"ref": ref, "kind": "connector", "type": t.Type, "source": t.Source, "online": false}, nil
		}
	}
	return nil, fmt.Errorf("there is no connector type %q; search them with forge.catalog {area: \"connectors\"}", id)
}
