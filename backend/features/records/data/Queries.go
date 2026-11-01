package data

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

type getParams struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

// ListParams are records.list's parameters.
type ListParams struct {
	Entity string `json:"entity"`
	// Where keeps the records whose fields equal these values (field id →
	// value, null for none); empty keeps all.
	Where  map[string]json.RawMessage `json:"where,omitempty"`
	Filter *Filter                    `json:"filter,omitempty"`
}

func entityResource(p features.Principal, params json.RawMessage) string {
	var q struct{ Entity string }
	_ = json.Unmarshal(params, &q)
	return records.Resource(p, q.Entity)
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "records.get", Source: "records"}, Title: "A record",
		Description: "One record: its values by field id, and _id, _entity, _seq (what record.update's expect names), _createdAt, _updatedAt, _createdBy, _updatedBy and _problems (fields whose value a type change cleared). null while there is none.",
		Params:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string"},"id":{"type":"string","description":"The record's id"}},"required":["entity","id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"entity":"task","id":"1234"}`)},
		Action:      "record.read", Resource: entityResource,
		Run: live.ValueOf(Projector, func(f live.Feed) (string, error) {
			var p getParams
			_ = json.Unmarshal(f.Params(), &p)
			if !live.ValidToken(p.Entity) || !live.ValidToken(p.ID) {
				return "", log.Code("record_not_found").Errorf("%q of %q is not a record", p.ID, p.Entity)
			}
			return Key(p.Entity, p.ID), nil
		}, json.RawMessage("null")),
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "records.list", Source: "records"}, Title: "Records",
		Description: "The records of an entity type, keyed <entityId>.<recordId>, each as records.get gives it, changing as records are created, changed and deleted. where keeps those whose fields equal the values given; filter is a condition {field, op, value} (op " + strings.Join(Ops, ", ") + ") or {and: [...]} / {or: [...]} of them. It is answered from an index when one fits, and otherwise reads every record of the type, up to the records section's scanLimit. Sort on the client; records.query reads sorted pages once.",
		Params:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string"},"where":{"type":"object","description":"Field id → value the records must have"},"filter":{"type":"object","description":"{field, op, value}, or {and: [filters]} or {or: [filters]}"}},"required":["entity"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"entity":"task","where":{"done":false}}`), json.RawMessage(`{"entity":"task","filter":{"field":"due","op":"lt","value":"2026-12-01"}}`)},
		Action:      "record.read", Resource: entityResource,
		Run: func(f live.Feed) error {
			d := deps()
			args := f.Args()
			if err := Follow(args, d); err != nil {
				log.Error(args.Context(), err, "the converter cannot start")
			}
			var p ListParams
			_ = json.Unmarshal(f.Params(), &p)
			pl, err := prepare(args, d, Query{Entity: p.Entity, Where: p.Where, Filter: p.Filter})
			if err != nil {
				return err
			}
			// Changes are followed from before the snapshot is read, so none
			// is missed; one the snapshot already has is sent again
			updates, err := d.Projections.Updates(f.Context(), args.Principal.TenantID, Projector, p.Entity+".*")
			if err != nil {
				return err
			}
			// The snapshot reflects every event folded before it is read
			folded, err := d.Projections.Folded(f.Context(), args.Principal.TenantID, Projector)
			if err != nil {
				return err
			}
			res, err := pl.run(args, d)
			if err != nil {
				return err
			}
			if !res.Plan.Complete {
				return log.Code("scan_limit").With("entity", p.Entity).Errorf("%s has more than %d records, more than a list reads without an index", p.Entity, d.settings(args).ScanLimit)
			}
			shown := map[string]bool{}
			initial := map[string]json.RawMessage{}
			for _, r := range res.Records {
				var id string
				_ = json.Unmarshal(r[MetaID], &id)
				raw, _ := json.Marshal(r)
				initial[Key(p.Entity, id)], shown[Key(p.Entity, id)] = raw, true
			}
			pos := folded
			f.Snapshot(initial, pos)
			for e := range updates {
				pos = pos.Merge(e.Value.Position())
				var r Record
				keep := !e.Deleted && json.Unmarshal(e.Value.Value, &r) == nil && pl.filter.match(r)
				switch {
				case keep:
					shown[e.Key] = true
					f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: e.Value.Value}, pos)
				case shown[e.Key]:
					delete(shown, e.Key)
					f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
				default:
					// Once caught up, say how far the list has reached
					if len(updates) == 0 {
						f.Change(live.Reached, pos)
					}
				}
			}
			return nil
		},
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "records.find", Source: "records"}, Title: "A page of records",
		Description: "A sorted page of an entity type's records that stays current: {records, next, plan}, as records.query reads it once. where and filter are records.list's; sort is [{field, desc}] (record id order without one); limit is the page's size (every match when left out) and cursor a previous next. Changes to records are insert, update, move and remove on the records list, with the index they apply at, and next follows the page's last record. A page after a cursor keeps its place when records enter before it. Without an index giving the sort, every match is read once, up to the records section's scanLimit.",
		Params:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string"},"where":{"type":"object","description":"Field id → value the records must have"},"filter":{"type":"object","description":"{field, op, value}, or {and: [filters]} or {or: [filters]}"},"sort":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"desc":{"type":"boolean"}},"required":["field"]}},"limit":{"type":"integer","description":"The page's size; every match when left out"},"cursor":{"type":"string","description":"A previous result's next, for the following page"},"fields":{"type":"array","items":{"type":"string"},"description":"The field ids to return; all when left out"}},"required":["entity"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"entity":"task","where":{"done":false},"sort":[{"field":"due"}],"limit":20}`)},
		Action:      "record.read", Resource: entityResource,
		Run: runFind(deps),
	})
}
