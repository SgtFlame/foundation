package prefabs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"gorm.io/gorm"
)

// View is the prefab.describe query's result.
type View struct {
	Prefab       ui.Prefab `json:"prefab"`
	Rev          int       `json:"rev"` // 0 for a published version
	Published    int       `json:"published"`
	PublishedRev int       `json:"publishedRev"`
	Changed      bool      `json:"changed"` // the draft has changed since it was published
	UpdatedBy    uint64    `json:"updatedBy,string,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitzero"`
	Deleted      bool      `json:"deleted,omitempty"` // hidden until prefab.restore
}

// Summary is one prefab in the prefab.list query.
type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Rev         int    `json:"rev"`
	Published   int    `json:"published"`
	Changed     bool   `json:"changed"`           // changed since it was published
	Deleted     bool   `json:"deleted,omitempty"` // hidden until prefab.restore: list it apart
}

func viewOf(d Draft) View {
	return View{Prefab: d.Prefab, Rev: d.Rev, Published: d.Published, PublishedRev: d.PublishedRev,
		Changed: d.Rev != d.PublishedRev, UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt, Deleted: d.Deleted}
}

func summaryOf(d Draft) Summary {
	p := d.Prefab
	return Summary{ID: p.ID, Name: p.Name, Description: p.Description, Owner: p.Owner, Rev: d.Rev, Published: d.Published, Changed: d.Rev != d.PublishedRev, Deleted: d.Deleted}
}

type describeParams struct {
	ID        string `json:"id"`
	Published bool   `json:"published"`
	Version   int    `json:"version"`
}

func paramsOf(raw json.RawMessage) (describeParams, error) {
	var p describeParams
	_ = json.Unmarshal(raw, &p)
	if !live.ValidToken(p.ID) {
		return p, fmt.Errorf("%q is not a prefab id", p.ID)
	}
	return p, nil
}

// viewAt reads a projected value as a View: a draft, or a published version.
func viewAt(raw json.RawMessage, published bool) (View, bool) {
	if raw == nil {
		return View{}, false
	}
	if published {
		var p ui.Prefab
		if json.Unmarshal(raw, &p) != nil {
			return View{}, false
		}
		return View{Prefab: p, Published: p.Version}, true
	}
	var d Draft
	if json.Unmarshal(raw, &d) != nil {
		return View{}, false
	}
	return viewOf(d), true
}

// Version reads one published version from the tenant database.
func Version(args features.ServiceArgs, deps Deps, id string, version int) (ui.Prefab, ui.PrefabVersion, error) {
	var row ui.PrefabVersion
	if err := Follow(args, deps); err != nil {
		log.Error(args.Context(), err, "cannot follow the published prefabs")
	}
	q := deps.DB.Model(&ui.PrefabVersion{}).Scopes(db.InTenant(args.Principal.TenantID)).Where("prefab_id = ?", id)
	if version > 0 {
		q = q.Where("version = ?", version)
	}
	err := q.Order("version DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ui.Prefab{}, row, log.Code("prefab_not_found").With("prefabId", id).With("version", version).Errorf("there is no published prefab %q of that version", id)
	}
	if err != nil {
		return ui.Prefab{}, row, log.Wrap(err)
	}
	var p ui.Prefab
	if err := json.Unmarshal([]byte(row.Definition), &p); err != nil {
		return p, row, log.Wrap(err)
	}
	return p, row, nil
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "prefab.describe", Source: "ui"}, Title: "A prefab",
		Description: "A prefab's draft as it is edited: {prefab: {id, name, description, owner, props, events, slots, state, queries, template, graphs, preview, version}, rev, published, publishedRev, changed}. The template is a tree of nodes {type, key, props, bind, on, children, slots}, found by key paths such as root/row/title. With published, its last published version instead; with version, that published version (which never changes). Changes with every prefab.* command.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The prefab's id"},"published":{"type":"boolean","description":"The last published version rather than the draft"},"version":{"type":"integer","description":"A published version"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"taskRow"}`)},
		Action:      "prefabs.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			q, _ := paramsOf(params)
			return ui.PrefabResource(p, q.ID)
		},
		Run: func(f live.Feed) error {
			q, err := paramsOf(f.Params())
			if err != nil {
				return err
			}
			if q.Version > 0 {
				p, row, err := Version(f.Args(), deps(), q.ID, q.Version)
				if err != nil {
					return err
				}
				f.Snapshot(View{Prefab: p, Published: row.Version}, live.Position{row.Stream: row.Seq})
				<-f.Context().Done()
				return nil
			}
			projector := DraftProjector
			if q.Published {
				projector = PublishedProjector
			}
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, projector, TargetType+"."+q.ID)
			if err != nil {
				return err
			}
			var v View
			found, started := false, false
			pos := live.Position{}
			for e := range entries {
				if e == nil {
					started = true
					if !found {
						return log.Code("prefab_not_found").With("prefabId", q.ID).Errorf("there is no prefab %q", q.ID)
					}
					f.Snapshot(v, pos)
					continue
				}
				pos = pos.Merge(e.Value.Position())
				nv, ok := viewAt(e.Value.Value, q.Published)
				if !ok {
					continue
				}
				v, found = nv, true
				if started {
					raw, _ := json.Marshal(v)
					f.Change(live.Delta{Op: "replace", Value: raw}, pos)
				}
			}
			return nil
		},
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "prefab.list", Source: "ui"}, Title: "Prefabs",
		Description: "The tenant's prefab library, by key \"prefab.<id>\": {id, name, description, owner, rev, published, changed}. owner narrows it to one app's (app:<appId>) or, as \"tenant\", to the tenant library's (no owner); search keeps those whose name or description holds the text. Who is editing each one is presence on prefab.<id>.",
		Params:      json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","description":"app:<appId>, or tenant for the tenant library"},"search":{"type":"string","description":"Text the name or description holds"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"owner":"app:hello"}`)},
		Action:      "prefabs.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return ui.PrefabResource(p, "") },
		Run: func(f live.Feed) error {
			var p struct{ Owner, Search string }
			_ = json.Unmarshal(f.Params(), &p)
			search := strings.ToLower(strings.TrimSpace(p.Search))
			keep := func(s Summary) bool {
				switch {
				case p.Owner == "tenant" && s.Owner != "":
					return false
				case p.Owner != "" && p.Owner != "tenant" && s.Owner != p.Owner:
					return false
				}
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
