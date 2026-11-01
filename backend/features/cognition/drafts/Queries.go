package drafts

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// View is the cognition.graph query's result: a graph's draft, resolved.
type View struct {
	Graph       cognition.Graph           `json:"graph"`
	Pins        map[string]cognition.Pins `json:"pins"`
	Problems    []cognition.Problem       `json:"problems"`
	Conversions []EdgeConversion          `json:"conversions"`
	// Rev is 0 for a built-in no command has changed in the tenant.
	Rev          int       `json:"rev"`
	Published    int       `json:"published"`
	PublishedRev int       `json:"publishedRev"`
	BuiltIn      bool      `json:"builtIn"`
	ReadOnly     bool      `json:"readOnly"` // a built-in that is not editable
	UpdatedBy    uint64    `json:"updatedBy,string,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitzero"`
	Untraced     int64     `json:"untraced"` // runs not traced since the server started
}

// EdgeConversion is an implicit conversion on the edge into an input.
type EdgeConversion struct {
	Node string `json:"node"`
	Pin  string `json:"pin"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Summary is one graph in the cognition.graphs query.
type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Owner       string `json:"owner,omitempty"`
	Published   int    `json:"published"`
	Rev         int    `json:"rev"`
	Changed     bool   `json:"changed"` // the draft has changed since it was published
	BuiltIn     bool   `json:"builtIn"`
	ReadOnly    bool   `json:"readOnly"`
}

func view(args features.ServiceArgs, deps Deps, id string, d *Draft) View {
	b, builtIn := deps.Engine.BuiltIn(id)
	v := View{BuiltIn: builtIn, ReadOnly: builtIn && !b.Editable}
	switch {
	case d != nil:
		v.Graph, v.Rev, v.Published, v.PublishedRev, v.UpdatedBy, v.UpdatedAt = d.Graph, d.Rev, d.Published, d.PublishedRev, d.UpdatedBy, d.UpdatedAt
	case builtIn:
		v.Graph = b.Graph
		v.Published = b.Graph.Version
	}
	res := deps.Engine.Resolve(v.Graph)
	v.Pins, v.Problems = res.Pins, res.Problems
	// No pins is an empty list for the browser, not null
	for id, p := range v.Pins {
		if p.In == nil {
			p.In = []cognition.Pin{}
		}
		if p.Out == nil {
			p.Out = []cognition.Pin{}
		}
		v.Pins[id] = p
	}
	if v.Problems == nil {
		v.Problems = []cognition.Problem{}
	}
	v.Conversions = []EdgeConversion{}
	for to, c := range res.Conversions {
		v.Conversions = append(v.Conversions, EdgeConversion{Node: to.Node, Pin: to.Pin, From: c.From, To: c.To})
	}
	slices.SortFunc(v.Conversions, func(a, b EdgeConversion) int { return cmp.Or(cmp.Compare(a.Node, b.Node), cmp.Compare(a.Pin, b.Pin)) })
	v.Untraced = deps.Engine.Untraced(args, id)
	return v
}

func summaryOf(id string, d *Draft, b *cognition.BuiltIn) Summary {
	s := Summary{ID: id, BuiltIn: b != nil, ReadOnly: b != nil && !b.Editable}
	g := cognition.Graph{}
	if d != nil {
		g = d.Graph
		s.Published, s.Rev, s.Changed = d.Published, d.Rev, d.Rev != d.PublishedRev
	} else if b != nil {
		g = b.Graph
		s.Published = g.Version
	}
	s.Name, s.Description, s.Kind, s.Owner = g.Name, g.Description, g.Kind, g.Owner
	return s
}

func idOf(params json.RawMessage) (string, error) {
	var p struct{ ID string }
	_ = json.Unmarshal(params, &p)
	if !live.ValidToken(p.ID) {
		return "", fmt.Errorf("%q is not a graph id", p.ID)
	}
	return p.ID, nil
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "cognition.graph", Source: "cognition"}, Title: "A Cognition graph",
		Description: "A graph's draft as it is edited, resolved: {graph: {id, name, kind, spec, variables, nodes, edges}, pins (each node's pins by node id), problems, conversions, rev, published, builtIn, readOnly}. A built-in no one has changed is returned as built in. Changes with every bp.* command.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The graph's id"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"personal"}`)},
		Action:      "cognition.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			id, _ := idOf(params)
			return Resource(p, id)
		},
		Run: func(f live.Feed) error {
			id, err := idOf(f.Params())
			if err != nil {
				return err
			}
			d := deps()
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, DraftProjector, TargetType+"."+id)
			if err != nil {
				return err
			}
			var draft *Draft
			pos := live.Position{}
			started := false
			for e := range entries {
				if e != nil {
					pos = pos.Merge(e.Value.Position())
					draft = nil
					if !e.Deleted {
						var x Draft
						if err := json.Unmarshal(e.Value.Value, &x); err == nil {
							draft = &x
						}
					}
					if !started {
						continue
					}
				}
				v := view(f.Args(), d, id, draft)
				if !started {
					started = true
					if draft == nil && !v.BuiltIn {
						return log.Code("graph_not_found").With("graphId", id).Errorf("there is no graph %q", id)
					}
					f.Snapshot(v, pos)
					continue
				}
				raw, _ := json.Marshal(v)
				f.Change(live.Delta{Op: "replace", Value: raw}, pos)
			}
			return nil
		},
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "cognition.graphs", Source: "cognition"}, Title: "Cognition graphs",
		Description: "Every Cognition graph of the tenant and every built-in, by key \"graph.<id>\": {id, name, description, kind, owner, published, rev, changed, builtIn, readOnly}. kind narrows it to one kind.",
		Params:      json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","description":"Only graphs of this kind"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"kind":"ui.handler"}`)},
		Action:      "cognition.read",
		Run: func(f live.Feed) error {
			var p struct{ Kind string }
			_ = json.Unmarshal(f.Params(), &p)
			d := deps()
			builtIns := map[string]*cognition.BuiltIn{}
			for _, b := range cognition.Graphs.All(d.Engine.Registry()) {
				b.Graph.ID = b.ID
				builtIns[b.ID] = &b
			}
			keep := func(s Summary) bool { return p.Kind == "" || s.Kind == p.Kind }
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, DraftProjector, TargetType+".*")
			if err != nil {
				return err
			}
			initial := map[string]Summary{}
			pos := live.Position{}
			started := false
			for e := range entries {
				if e == nil {
					for id, b := range builtIns {
						if _, ok := initial[TargetType+"."+id]; !ok {
							if s := summaryOf(id, nil, b); keep(s) {
								initial[TargetType+"."+id] = s
							}
						}
					}
					started = true
					f.Snapshot(initial, pos)
					continue
				}
				pos = pos.Merge(e.Value.Position())
				id := e.Key[len(TargetType)+1:]
				var s Summary
				gone := e.Deleted
				if !gone {
					var x Draft
					if json.Unmarshal(e.Value.Value, &x) != nil {
						continue
					}
					s = summaryOf(id, &x, builtIns[id])
				} else if b := builtIns[id]; b != nil {
					s, gone = summaryOf(id, nil, b), false
				}
				if !gone && !keep(s) {
					gone = true
				}
				switch {
				case !started && gone:
					delete(initial, e.Key)
				case !started:
					initial[e.Key] = s
				case gone:
					f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
				default:
					raw, _ := json.Marshal(s)
					f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: raw}, pos)
				}
			}
			return nil
		},
	})
}

// Source gives the engine the tenants' published graphs.
type Source struct{ Store live.Store }

// Published reads the cognition.published projection.
func (s Source) Published(args features.ServiceArgs, id string) (cognition.Graph, bool, error) {
	v, ok, err := s.Store.Get(args.Context(), args.Principal.TenantID, PublishedProjector, TargetType+"."+id)
	if err != nil || !ok || v.Value == nil {
		return cognition.Graph{}, false, err
	}
	var g cognition.Graph
	if err := json.Unmarshal(v.Value, &g); err != nil {
		return cognition.Graph{}, false, err
	}
	return g, true, nil
}
