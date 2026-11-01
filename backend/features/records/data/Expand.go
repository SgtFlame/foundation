package data

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// MetaExpanded is where a query's expansions put the records a relation
// points to: _expanded.<field> is one record (or null), or for a to-many
// relation the list of them in the field's order (Records.md › Built in
// slice 20). It is never stored.
const MetaExpanded = "_expanded"

// expansion is one relation field a query follows.
type expansion struct {
	field  records.Field
	target string             // the related entity type, or records.UserEntity
	e      records.EntityType // the target's published version, for its read rule
}

// expansions checks a query's expand: visible relation fields, each once,
// whose target is published (or user).
func expansions(args features.ServiceArgs, d Deps, e records.EntityType, ids []string) ([]expansion, error) {
	var out []expansion
	for _, id := range ids {
		f, _, ok := e.FieldByID(id)
		if !ok || f.Hidden || f.Type != "relation" || f.Relation == nil {
			return nil, log.Code("bad_expand").Errorf("%s has no relation field %q to expand", e.Name, id)
		}
		if slices.ContainsFunc(out, func(x expansion) bool { return x.field.ID == id }) {
			return nil, log.Code("bad_expand").Errorf("%s is expanded twice", id)
		}
		x := expansion{field: f, target: f.Relation.Entity}
		if x.target != records.UserEntity {
			t, problems := Entity(args, d.Projections, x.target)
			if problems != nil {
				return nil, log.Code("bad_expand").Errorf("%s cannot be expanded: %s", id, problems[0].Message)
			}
			x.e = t
		}
		out = append(out, x)
	}
	return out, nil
}

// targets are the entity types the query reads: its own and the ones it
// expands to (users commit no events, so they are not among them).
func (p *planned) targets() []string {
	out := []string{p.e.ID}
	for _, x := range p.expand {
		if x.target != records.UserEntity && !slices.Contains(out, x.target) {
			out = append(out, x.target)
		}
	}
	return out
}

// expandAll puts each expansion's related records under _expanded, in
// copies of a page's records. The relations are read from the page's rows,
// which hold every field even when the query returns only some. A related
// record the target's read rule hides from the principal is left out, as
// one that is gone is.
func (p *planned) expandAll(args features.ServiceArgs, d Deps, recs []Record, rows []row) error {
	if len(p.expand) == 0 {
		return nil
	}
	for i := range recs {
		recs[i] = maps.Clone(recs[i])
	}
	for _, x := range p.expand {
		var ids []string
		for _, r := range rows[:len(recs)] {
			for _, id := range relatedIDs(x.field, r.record[x.field.ID]) {
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		found, err := p.related(args, d, x, ids)
		if err != nil {
			return err
		}
		for i, r := range recs {
			exp := map[string]any{}
			_ = json.Unmarshal(r[MetaExpanded], &exp)
			ids := relatedIDs(x.field, rows[i].record[x.field.ID])
			if x.field.Relation.Many {
				list := []Record{}
				for _, id := range ids {
					if v, ok := found[id]; ok {
						list = append(list, v)
					}
				}
				exp[x.field.ID] = list
			} else if len(ids) == 1 && found[ids[0]] != nil {
				exp[x.field.ID] = found[ids[0]]
			} else {
				exp[x.field.ID] = nil
			}
			r[MetaExpanded] = raw(exp)
		}
	}
	return nil
}

// relatedIDs are the ids a relation value holds.
func relatedIDs(f records.Field, v json.RawMessage) []string {
	if records.IsNull(v) {
		return nil
	}
	if f.Relation.Many {
		var ids []string
		_ = json.Unmarshal(v, &ids)
		return ids
	}
	var id string
	if json.Unmarshal(v, &id) != nil {
		return nil
	}
	return []string{id}
}

// related reads the records an expansion names, by id.
func (p *planned) related(args features.ServiceArgs, d Deps, x expansion, ids []string) (map[string]Record, error) {
	out := map[string]Record{}
	if x.target == records.UserEntity {
		if d.Users == nil || len(ids) == 0 {
			return out, nil
		}
		var nums []uint64
		for _, id := range ids {
			if n, err := strconv.ParseUint(id, 10, 64); err == nil {
				nums = append(nums, n)
			}
		}
		for n, u := range d.Users(args.Principal.TenantID, nums) {
			id := strconv.FormatUint(n, 10)
			out[id] = Record{MetaID: raw(id), MetaEntity: raw(records.UserEntity), "username": raw(u.Username), "displayName": raw(u.DisplayName)}
		}
		return out, nil
	}
	rule := x.e.Rule("read")
	for _, id := range ids {
		r, _, err := get(args, d, x.target, id)
		if err != nil {
			return nil, log.Wrap(fmt.Errorf("expanding %s: %w", x.field.ID, err))
		}
		if r != nil && records.Allows(rule, r, args.Principal) {
			out[id] = r
		}
	}
	return out, nil
}
