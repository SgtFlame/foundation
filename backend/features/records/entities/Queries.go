package entities

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// View is the entity.describe query's result.
type View struct {
	Entity       records.EntityType `json:"entity"`
	Rev          int                `json:"rev"` // 0 for a published version
	Published    int                `json:"published"`
	PublishedRev int                `json:"publishedRev"`
	Changed      bool               `json:"changed"` // the draft has changed since it was published
	UpdatedBy    uint64             `json:"updatedBy,string,omitempty"`
	UpdatedAt    time.Time          `json:"updatedAt,omitzero"`
}

// Summary is one entity type in the entity.list query.
type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Fields      int    `json:"fields"` // visible fields
	Rev         int    `json:"rev"`
	Published   int    `json:"published"`
	Changed     bool   `json:"changed"`
	Hidden      bool   `json:"hidden,omitempty"` // deleted, restorable
}

func viewOf(d Draft) View {
	return View{Entity: d.Entity, Rev: d.Rev, Published: d.Published, PublishedRev: d.PublishedRev,
		Changed: d.Rev != d.PublishedRev, UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt}
}

func summaryOf(d Draft) Summary {
	e := d.Entity
	return Summary{ID: e.ID, Name: e.Name, Description: e.Description, Owner: e.Owner, Fields: len(e.Visible()),
		Rev: d.Rev, Published: d.Published, Changed: d.Rev != d.PublishedRev, Hidden: e.Hidden}
}

type describeParams struct {
	ID        string `json:"id"`
	Published bool   `json:"published"`
}

func paramsOf(raw json.RawMessage) (describeParams, error) {
	var p describeParams
	_ = json.Unmarshal(raw, &p)
	if !live.ValidToken(p.ID) {
		return p, fmt.Errorf("%q is not an entity type id", p.ID)
	}
	return p, nil
}

// viewAt reads a projected value as a View: a draft, or a published version.
func viewAt(raw json.RawMessage, published bool) (View, bool) {
	if raw == nil {
		return View{}, false
	}
	if published {
		var e records.EntityType
		if json.Unmarshal(raw, &e) != nil {
			return View{}, false
		}
		return View{Entity: e, Published: e.Version}, true
	}
	var d Draft
	if json.Unmarshal(raw, &d) != nil {
		return View{}, false
	}
	return viewOf(d), true
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "entity.describe", Source: "records"}, Title: "An entity type",
		Description: "An entity type's draft as it is edited: {entity: {id, name, description, owner, titleField, fields: [{id, name, type, required, hidden, …}], indexes, access, version}, rev, published, publishedRev, changed}. Removed fields are there with hidden: true. With published, its last published version instead. Changes with every entity.* command.",
		Params:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The entity type's id"},"published":{"type":"boolean","description":"The last published version rather than the draft"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"task"}`)},
		Action:      "records.read",
		Resource: func(p features.Principal, params json.RawMessage) string {
			q, _ := paramsOf(params)
			return records.Resource(p, q.ID)
		},
		Run: func(f live.Feed) error {
			q, err := paramsOf(f.Params())
			if err != nil {
				return err
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
						return log.Code("entity_not_found").With("entityId", q.ID).Errorf("there is no entity type %q", q.ID)
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
		Descriptor: extensions.Descriptor{ID: "entity.list", Source: "records"}, Title: "Entity types",
		Description: "Every entity type of the tenant, by key \"entity.<id>\": {id, name, description, owner, fields (how many), rev, published, changed, hidden (deleted)}. owner narrows it to one app's (app:<appId>) or the shared ones (tenant).",
		Params:      json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","description":"Only entity types with this owner"}}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"owner":"app:hello"}`)},
		Action:      "records.read",
		Run: func(f live.Feed) error {
			var p struct{ Owner string }
			_ = json.Unmarshal(f.Params(), &p)
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, DraftProjector, TargetType+".*")
			if err != nil {
				return err
			}
			initial := map[string]Summary{}
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
					gone = p.Owner != "" && s.Owner != p.Owner
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
