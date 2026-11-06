package data

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Computed values are written by events (Forge 03-DataModel question 9,
// Records.md › Built in slice 24): after a record command decides its
// change, compute works out the record's computed fields as they will be
// and puts those that differ in the record's last event. A create takes
// the next autoNumber from the entity type's counter, whose counted event
// commits with it in one batch, so concurrent creates get distinct numbers
// with no gaps. record.recompute writes them again when a formula or rollup
// is published anew or the records a rollup reads change.

// after is the record as it will be once events are folded; nil once
// deleted.
func (c *decision) after(events []live.NewEvent) (Record, error) {
	r := maps.Clone(c.record)
	for _, ev := range events {
		if ev.Target != nil && *ev.Target != c.Target {
			continue
		}
		data, _ := json.Marshal(ev.Data)
		next, err := Apply(r, live.Event{Type: ev.Type, Target: c.Target, Data: data, UserID: c.Principal.UserID, TenantID: c.Principal.TenantID, Time: time.Now()})
		if err != nil {
			return nil, err
		}
		r = next
	}
	return r, nil
}

// compute completes a record command's events with the record's computed
// values.
func (c *decision) compute(events []live.NewEvent, create bool) ([]live.NewEvent, []live.Problem) {
	var fields []records.Field
	for _, f := range c.entity.Visible() {
		if records.IsComputed(f) {
			fields = append(fields, f)
		}
	}
	last := -1
	for i, ev := range events {
		if _, ok := ev.Data.(Change); ok && (ev.Target == nil || *ev.Target == c.Target) {
			last = i
		}
	}
	if len(fields) == 0 || last < 0 {
		return events, nil
	}
	r, err := c.after(events)
	if err != nil {
		return nil, problem("", err.Error(), "")
	}
	if r == nil {
		return events, nil
	}
	recomputed := events[last].Type == EventRecomputed
	values, meta := map[string]json.RawMessage{}, map[string]string{}
	for _, f := range fields {
		var v json.RawMessage
		switch f.Type {
		case "createdAt", "createdBy", "updatedAt":
			// Folding sets the record's own data: the field takes it then
			if create || r[f.ID] == nil || f.Type == "updatedAt" && !recomputed {
				meta[f.ID] = records.MetaOf(f)
				r[f.ID] = r[records.MetaOf(f)]
			}
			continue
		case "autoNumber":
			if !create {
				continue // records made before the field have no number
			}
			counter := live.Target{Area: live.AreaRecords, Type: CounterType, ID: c.entity.ID}
			state, err := c.readTarget(counter)
			if err != nil {
				return nil, problem("", "the counter cannot be read: "+err.Error(), "")
			}
			var n Counter
			if state != nil {
				_ = json.Unmarshal(state, &n)
			}
			n.N++
			events = append(events, live.NewEvent{Type: EventCounted, Data: n, Target: &counter})
			v = raw(n.N)
		case "formula":
			v = records.EvalFormula(c.entity, f, r)
		case "rollup":
			v = c.rollup(f, r)
		}
		if !same(v, r[f.ID]) {
			values[f.ID] = orNull(v)
			if records.IsNull(v) {
				delete(r, f.ID)
			} else {
				r[f.ID] = v
			}
		}
	}
	if len(values) == 0 && len(meta) == 0 {
		if recomputed {
			return nil, nil // nothing changed
		}
		return events, nil
	}
	ch := events[last].Data.(Change)
	if len(values) > 0 {
		ch.Computed = values
	}
	if len(meta) > 0 {
		ch.Meta = meta
	}
	events[last].Data = ch
	return events, nil
}

// counterOf is the autoNumber counter a create takes a number from, if its
// entity type has one: the bus holds it before deciding, so the creates of
// one process take numbers in turn rather than conflicting.
func counterOf(d live.Decision, deps Deps) []live.Target {
	if deps.Projections == nil || d.Target.Type == "" {
		return nil
	}
	e, problems := Entity(d.ServiceArgs, deps.Projections, d.Target.Type)
	if problems != nil || !slices.ContainsFunc(e.Visible(), func(f records.Field) bool { return f.Type == "autoNumber" }) {
		return nil
	}
	return []live.Target{{Area: live.AreaRecords, Type: CounterType, ID: e.ID}}
}

// readTarget is another target's state in the records projection, read
// with the command's Decision.Read, so the commit expects it unchanged.
func (c *decision) readTarget(t live.Target) (json.RawMessage, error) {
	if c.Read != nil {
		return c.Read(Projector, t)
	}
	v, ok, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, Projector, t.Key())
	if err != nil || !ok {
		return nil, err
	}
	return v.Value, nil
}

// rollup is a rollup field's value for record r: its aggregate over the
// records its relation field points to that exist (row rules aside).
func (c *decision) rollup(f records.Field, r Record) json.RawMessage {
	if f.Rollup == nil {
		return nil
	}
	rel, _, ok := c.entity.FieldByID(f.Rollup.Relation)
	if !ok || rel.Relation == nil {
		return nil
	}
	ids := relationIDs(r[rel.ID])
	if rel.Relation.Entity == records.UserEntity {
		if f.Rollup.Aggregate != "count" {
			return nil
		}
		return raw(len(ids))
	}
	related := c.entity
	if rel.Relation.Entity != c.entity.ID {
		var problems []live.Problem
		if related, problems = Entity(features.ServiceArgs{Ctx: c.Context(), Principal: features.ServicePrincipal(c.Principal.TenantID)}, c.deps.Projections, rel.Relation.Entity); problems != nil {
			return nil
		}
	}
	acc := &accumulator{c: compiledAgg{Aggregate: Aggregate{Op: f.Rollup.Aggregate, Field: f.Rollup.Field}}, sum: new(big.Rat)}
	if f.Rollup.Field != "" {
		of, _, ok := related.FieldByID(f.Rollup.Field)
		if !ok {
			return nil
		}
		acc.c.field = records.ValueField(of)
	}
	for _, id := range ids {
		if id == c.Target.ID && rel.Relation.Entity == c.entity.ID {
			acc.add(r)
			continue
		}
		v, ok, err := c.deps.Projections.Get(c.Context(), c.Principal.TenantID, Projector, Key(rel.Relation.Entity, id))
		if err != nil || !ok || v.Value == nil {
			continue
		}
		var x Record
		if json.Unmarshal(v.Value, &x) == nil {
			acc.add(x)
		}
	}
	return acc.value()
}

// relationIDs are the record ids a relation value holds.
func relationIDs(raw json.RawMessage) []string {
	if records.IsNull(raw) {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	_ = json.Unmarshal(raw, &many)
	return many
}

// computedFields are the ids of an entity type's computed fields.
func computedFields(e records.EntityType) []string {
	var out []string
	for _, f := range e.Visible() {
		if records.IsComputed(f) {
			out = append(out, f.ID)
		}
	}
	return out
}

// rollupsOver are the rollups that read records of entity, among types:
// the type holding each and its relation field.
func rollupsOver(types []records.EntityType, entity string) []rollupRef {
	var out []rollupRef
	for _, t := range types {
		for _, f := range t.Visible() {
			if f.Type != "rollup" || f.Rollup == nil {
				continue
			}
			rel, _, ok := t.FieldByID(f.Rollup.Relation)
			if !ok || rel.Relation == nil || rel.Relation.Entity != entity {
				continue
			}
			ref := rollupRef{entity: t.ID, relation: rel}
			if !slices.ContainsFunc(out, func(x rollupRef) bool { return x.entity == ref.entity && x.relation.ID == ref.relation.ID }) {
				out = append(out, ref)
			}
		}
	}
	return out
}

// rollupRef is a relation field some rollup aggregates over.
type rollupRef struct {
	entity   string
	relation records.Field
}

// isCounter reports whether a projection key is an autoNumber counter's.
func isCounter(key string) bool { return strings.HasPrefix(key, CounterType+".") }

// checkFiles refuses files set on the record that do not exist or that
// someone else uploaded: a file's id alone gives no one its content.
func (c *decision) checkFiles(events []live.NewEvent) []live.Problem {
	var fields []records.Field
	for _, f := range c.entity.Visible() {
		if item, _ := records.ItemOf(f); f.Type == "file" || item.Type == "file" {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	r, err := c.after(events)
	if err != nil || r == nil {
		return nil
	}
	for _, f := range fields {
		had := records.FilesOf(f, c.record[f.ID])
		for _, ref := range records.FilesOf(f, r[f.ID]) {
			if slices.ContainsFunc(had, func(x records.FileRef) bool { return x.ID == ref.ID }) {
				continue
			}
			if c.deps.Files == nil {
				return problem("args", "files cannot be kept here", "")
			}
			user, ok, err := c.deps.Files.Uploader(c.Context(), c.Principal.TenantID, ref.ID)
			if err != nil {
				return problem("", "the file cannot be read: "+err.Error(), "")
			}
			if !ok || c.Principal.Kind != features.PrincipalService && user != c.Principal.UserID {
				return []live.Problem{{Code: "file_not_found", Path: "args", Message: fmt.Sprintf("%s: there is no file %s you uploaded", f.Name, ref.ID), Fix: "upload the file first (POST /records/files/upload, or the Upload file node) and set the reference it gives"}}
			}
		}
	}
	return nil
}
