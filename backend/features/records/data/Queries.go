package data

import (
	"encoding/json"
	"fmt"

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
	Where map[string]json.RawMessage `json:"where,omitempty"`
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
		Description: "The records of an entity type, keyed <entityId>.<recordId>, each as records.get gives it; where keeps those whose fields equal the values given. It reads every record of the type (no indexes yet), up to the records section's scanLimit, and changes as records are created, changed and deleted. Sort on the client.",
		Params:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string"},"where":{"type":"object","description":"Field id → value the records must have"}},"required":["entity"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"entity":"task","where":{"done":false}}`)},
		Action:      "record.read", Resource: entityResource,
		Run: func(f live.Feed) error {
			d := deps()
			args := f.Args()
			if err := Follow(args, d); err != nil {
				log.Error(args.Context(), err, "the converter cannot start")
			}
			var p ListParams
			_ = json.Unmarshal(f.Params(), &p)
			match, err := matcher(args, d, p)
			if err != nil {
				return err
			}
			entries, err := f.Store().Watch(f.Context(), args.Principal.TenantID, Projector, p.Entity+".*")
			if err != nil {
				return err
			}
			limit := d.settings(args).ScanLimit
			initial := map[string]json.RawMessage{}
			shown := map[string]bool{}
			pos := live.Position{}
			started, scanned := false, 0
			for e := range entries {
				if e == nil {
					started = true
					f.Snapshot(initial, pos)
					continue
				}
				pos = pos.Merge(e.Value.Position())
				if !started {
					if scanned++; scanned > limit {
						return log.Code("scan_limit").With("entity", p.Entity).Errorf("%s has more than %d records, more than a list reads without an index", p.Entity, limit)
					}
				}
				keep := !e.Deleted && match(e.Value.Value)
				switch {
				case !started && keep:
					initial[e.Key], shown[e.Key] = e.Value.Value, true
				case !started:
					delete(initial, e.Key)
					delete(shown, e.Key)
				case keep:
					shown[e.Key] = true
					f.Change(live.Delta{Op: "upsert", Key: e.Key, Value: e.Value.Value}, pos)
				case shown[e.Key]:
					delete(shown, e.Key)
					f.Change(live.Delta{Op: "remove", Key: e.Key}, pos)
				}
			}
			return nil
		},
	})
}

// matcher is records.list's filter: the where values, checked against
// their fields, compared with each record's.
func matcher(args features.ServiceArgs, d Deps, p ListParams) (func(json.RawMessage) bool, error) {
	if !live.ValidToken(p.Entity) {
		return nil, log.Code("entity_not_found").Errorf("%q is not an entity type id", p.Entity)
	}
	if len(p.Where) == 0 {
		return func(json.RawMessage) bool { return true }, nil
	}
	e, problems := Entity(args, d.Projections, p.Entity)
	if problems != nil {
		return nil, log.Code("entity_not_found").Errorf("%s", problems[0].Message)
	}
	want := map[string]json.RawMessage{}
	for id, v := range p.Where {
		f, _, ok := e.FieldByID(id)
		if !ok || f.Hidden {
			return nil, log.Code("bad_where").Errorf("%s has no field %q", e.Name, id)
		}
		out, err := records.CheckValue(f, v)
		if err != nil {
			return nil, log.Code("bad_where").Errorf("where %s: %v", id, fmt.Sprint(err))
		}
		want[id] = out
	}
	return func(raw json.RawMessage) bool {
		var r Record
		if json.Unmarshal(raw, &r) != nil {
			return false
		}
		for id, v := range want {
			if !same(r[id], v) {
				return false
			}
		}
		return true
	}, nil
}
