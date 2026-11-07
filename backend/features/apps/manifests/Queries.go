package manifests

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// View is the app.describe query's result: the manifest and a summary of
// each of its parts.
type View struct {
	Manifest  apps.Manifest `json:"manifest"`
	Rev       int           `json:"rev"`
	CreatedBy uint64        `json:"createdBy,string,omitempty"`
	UpdatedBy uint64        `json:"updatedBy,string,omitempty"`
	UpdatedAt time.Time     `json:"updatedAt,omitzero"`
	Parts     []Part        `json:"parts"`
}

// Part is a prefab, graph or entity type the manifest names or the app
// owns (forge 05a question 2).
type Part struct {
	Type      string `json:"type"` // prefab, graph or entity
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"` // a graph's kind
	Owner     string `json:"owner,omitempty"`
	Rev       int    `json:"rev,omitempty"`
	Published int    `json:"published,omitempty"`
	Changed   bool   `json:"changed,omitempty"` // changed since it was published
	Named     bool   `json:"named"`             // the manifest names it
	Owned     bool   `json:"owned"`             // its owner is this app
	Builtin   bool   `json:"builtin,omitempty"` // a built-in graph, with no draft
	Missing   bool   `json:"missing,omitempty"` // named, but not there
}

// Summary is one app in the app.list query.
type Summary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Icon        string    `json:"icon,omitempty"`
	Description string    `json:"description,omitempty"`
	Rev         int       `json:"rev"`
	Pages       int       `json:"pages"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
}

func summaryOf(d Draft) Summary {
	m := d.Manifest
	return Summary{ID: m.ID, Name: m.Name, Icon: m.Icon, Description: m.Description, Rev: d.Rev, Pages: len(m.Pages), UpdatedAt: d.UpdatedAt}
}

// named are the parts a manifest names, by type.
func named(m apps.Manifest) map[string][]string {
	out := map[string][]string{}
	add := func(typ, id string) {
		if id != "" && !slices.Contains(out[typ], id) {
			out[typ] = append(out[typ], id)
		}
	}
	if id, ok := strings.CutPrefix(m.Frame, "prefab."); ok {
		add("prefab", id)
	}
	for _, p := range m.Pages {
		add("prefab", p.Prefab)
	}
	for _, c := range m.Commands {
		add("graph", c.Graph)
	}
	for _, q := range m.Queries {
		add("graph", q.Graph)
	}
	for _, e := range m.Entities {
		add("entity", e)
	}
	return out
}

// partOf reads a draft of one of the part projections.
func partOf(typ string, raw json.RawMessage) (Part, bool) {
	var d struct {
		Rev          int             `json:"rev"`
		Published    int             `json:"published"`
		PublishedRev int             `json:"publishedRev"`
		Prefab       json.RawMessage `json:"prefab"`
		Graph        json.RawMessage `json:"graph"`
		Entity       json.RawMessage `json:"entity"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return Part{}, false
	}
	def := map[string]json.RawMessage{"prefab": d.Prefab, "graph": d.Graph, "entity": d.Entity}[typ]
	var x struct{ ID, Name, Kind, Owner string }
	if json.Unmarshal(def, &x) != nil || x.ID == "" {
		return Part{}, false
	}
	return Part{Type: typ, ID: x.ID, Name: x.Name, Kind: x.Kind, Owner: x.Owner, Rev: d.Rev, Published: d.Published, Changed: d.Rev != d.PublishedRev}, true
}

// partsOf are an app's parts: those it names and those it owns.
func partsOf(m apps.Manifest, known map[string]map[string]Part, builtins map[string]cognition.BuiltIn) []Part {
	out := []Part{}
	names := named(m)
	for _, typ := range []string{"prefab", "graph", "entity"} {
		seen := map[string]bool{}
		for _, id := range names[typ] {
			seen[id] = true
			p, ok := known[typ][id]
			switch {
			case ok:
			case typ == "graph" && builtins[id].ID != "":
				b := builtins[id]
				p = Part{Type: typ, ID: id, Name: b.Graph.Name, Kind: b.Graph.Kind, Owner: b.Graph.Owner, Builtin: true}
			default:
				p = Part{Type: typ, ID: id, Missing: true}
			}
			p.Named, p.Owned = true, p.Owner == apps.Owner(m.ID)
			out = append(out, p)
		}
		var owned []Part
		for id, p := range known[typ] {
			if !seen[id] && p.Owner == apps.Owner(m.ID) {
				p.Owned = true
				owned = append(owned, p)
			}
		}
		slices.SortFunc(owned, func(a, b Part) int { return strings.Compare(a.ID, b.ID) })
		out = append(out, owned...)
	}
	return out
}

func describeParams(raw json.RawMessage) (string, error) {
	var p struct{ ID string }
	_ = json.Unmarshal(raw, &p)
	if !live.ValidToken(p.ID) {
		return "", fmt.Errorf("%q is not an app id", p.ID)
	}
	return p.ID, nil
}

// watched is one entry of the watches app.describe holds.
type watched struct {
	src   string // app, prefab, graph or entity
	entry *live.Entry
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "app.describe", Source: "apps"}, Title: "An app",
		Description: "An app's draft as it is edited: {manifest: {id, name, icon, description, home, frame, pages, nav, editorKinds, entities, commands, queries, roles, settings, triggers, requires}, rev, parts}. parts are the prefabs, graphs and entity types the manifest names or the app owns (owner app:<appId>), each {type, id, name, kind, owner, rev, published, changed, named, owned, missing}. Changes with every app.* command and with every change to a part.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The app's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"tasks"}`)},
		Action:      "app.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			id, _ := describeParams(params)
			return apps.Resource(p, id)
		},
		Run: func(f live.Feed) error {
			id, err := describeParams(f.Params())
			if err != nil {
				return err
			}
			tenant := f.Args().Principal.TenantID
			// The draft, and every part's draft, so a part's change shows
			watches := []struct{ src, projector, pattern string }{
				{"app", DraftProjector, TargetType + "." + id},
				{"prefab", prefabs.DraftProjector, prefabs.TargetType + ".*"},
				{"graph", drafts.DraftProjector, drafts.TargetType + ".*"},
				{"entity", entities.DraftProjector, entities.TargetType + ".*"},
			}
			merged := make(chan watched)
			for _, w := range watches {
				entries, err := f.Store().Watch(f.Context(), tenant, w.projector, w.pattern)
				if err != nil {
					return err
				}
				go func() {
					for e := range entries {
						select {
						case merged <- watched{w.src, e}:
						case <-f.Context().Done():
							return
						}
					}
				}()
			}
			builtins := map[string]cognition.BuiltIn{}
			for _, b := range cognition.Graphs.All(reg) {
				builtins[b.ID] = b
			}
			known := map[string]map[string]Part{"prefab": {}, "graph": {}, "entity": {}}
			var d Draft
			found := false
			pending := len(watches)
			pos := live.Position{}
			last := ""
			view := func() View {
				return View{Manifest: d.Manifest, Rev: d.Rev, CreatedBy: d.CreatedBy, UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt, Parts: partsOf(d.Manifest, known, builtins)}
			}
			for {
				var w watched
				select {
				case w = <-merged:
				case <-f.Context().Done():
					return nil
				}
				if w.entry == nil {
					if pending--; pending > 0 {
						continue
					}
					if !found {
						return log.Code("app_not_found").With("appId", id).Errorf("there is no app %q", id)
					}
					v := view()
					raw, _ := json.Marshal(v)
					last = string(raw)
					f.Snapshot(v, pos)
					continue
				}
				e := w.entry
				pos = pos.Merge(e.Value.Position())
				if w.src == "app" {
					if e.Deleted || json.Unmarshal(e.Value.Value, &d) != nil {
						continue
					}
					found = true
				} else {
					key := strings.TrimPrefix(e.Key, map[string]string{"prefab": prefabs.TargetType, "graph": drafts.TargetType, "entity": entities.TargetType}[w.src]+".")
					if p, ok := partOf(w.src, e.Value.Value); ok && !e.Deleted {
						known[w.src][key] = p
					} else {
						delete(known[w.src], key)
					}
				}
				if pending > 0 {
					continue
				}
				raw, _ := json.Marshal(view())
				if string(raw) == last {
					// Only the app's own commands are waited for
					if w.src == "app" {
						f.Change(live.Reached, pos)
					}
					continue
				}
				last = string(raw)
				f.Change(live.Delta{Op: "replace", Value: raw}, pos)
			}
		},
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "app.list", Source: "apps"}, Title: "Apps",
		Description: "The tenant's apps, by key \"app.<id>\": {id, name, icon, description, rev, pages (how many), updatedAt}. search keeps those whose name or description holds the text. Who is editing each one is presence on app.<id>.",
		Params:      json.RawMessage(`{"type":"object","properties":{"search":{"type":"string","description":"Text the name or description holds"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "app.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return apps.Resource(p, "") },
		Run: func(f live.Feed) error {
			if err := Follow(f.Args(), deps()); err != nil {
				log.Error(f.Context(), err, "cannot follow the apps")
			}
			var p struct{ Search string }
			_ = json.Unmarshal(f.Params(), &p)
			search := strings.ToLower(strings.TrimSpace(p.Search))
			keep := func(s Summary) bool {
				return search == "" || strings.Contains(strings.ToLower(s.Name+"\n"+s.Description), search)
			}
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, DraftProjector, TargetType+".*")
			if err != nil {
				return err
			}
			initial := map[string]Summary{}
			shown := map[string]bool{}
			pos := live.Position{}
			started := false
			for e := range entries {
				if e == nil {
					started = true
					f.Snapshot(initial, pos)
					continue
				}
				pos = pos.Merge(e.Value.Position())
				var s Summary
				gone := e.Deleted
				if !gone {
					var d Draft
					if json.Unmarshal(e.Value.Value, &d) != nil {
						continue
					}
					s = summaryOf(d)
					gone = !keep(s)
				}
				switch {
				case !started && gone:
					delete(initial, e.Key)
				case !started:
					initial[e.Key], shown[e.Key] = s, true
				case gone:
					if shown[e.Key] {
						delete(shown, e.Key)
						f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
					}
				default:
					shown[e.Key] = true
					raw, _ := json.Marshal(s)
					f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: raw}, pos)
				}
			}
			return nil
		},
	})
}
