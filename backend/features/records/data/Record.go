// Package data keeps the records of entity types (documentation/Records.md
// › Built in slice 17): each record is an aggregate on the tenant's rec
// stream, at evt.<tenant>.rec.<entityId>.<recordId>.<event>, changed only
// by the generic record.* commands and folded by one projector per tenant
// into the records.record projection, keyed <entityId>.<recordId>. The
// records.get and records.list queries read it; records.list scans, until
// Forge's part 3d adds indexes. The converter follows published entity
// types and converts the values of fields whose type changed.
package data

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

var log = records.Log.Tags("data")

// Projector is the records' projector: every record of the tenant.
const Projector = "records.record"

// Events a record's commands commit. Each names the values it set and the
// ones they replaced, for undo and audit; the event itself carries the
// command, request and principal.
const (
	EventCreated   = "created"   // {values}
	EventUpdated   = "updated"   // {values, old}: only the changed fields; a null value clears one
	EventDeleted   = "deleted"   // {old}
	EventAdded     = "added"     // {field, items}: items added to a list-like field
	EventRemoved   = "removed"   // {field, items}: items removed from one
	EventConverted = "converted" // {values, old, problems}: a field's type changed
	// EventRecomputed changes only computed values (a formula or rollup
	// published anew, or records a rollup reads changed); it leaves
	// _updatedAt and _updatedBy as they were.
	EventRecomputed = "recomputed" // {computed, meta}
	// EventCounted is an autoNumber counter's next number, on the
	// counter's own target (CounterType), committed with the created event
	// that takes it.
	EventCounted = "counted" // {n}
)

// CounterType is the target type of the autoNumber counters, one per
// entity type, whose id is the entity type's (entity ids never start with
// '_'). The records.record projector keeps each as {n}, keyed
// _counter.<entityId>.
const CounterType = "_counter"

// Counter is an autoNumber counter's state and its counted event's data.
type Counter struct {
	N int64 `json:"n"`
}

// Change is an event's data.
type Change struct {
	Values   map[string]json.RawMessage `json:"values,omitempty"`
	Old      map[string]json.RawMessage `json:"old,omitempty"`
	Field    string                     `json:"field,omitempty"`
	Items    []json.RawMessage          `json:"items,omitempty"`
	Problems map[string]string          `json:"problems,omitempty"` // field id → why its value was cleared
	// Computed are computed fields' new values, null to clear one; the
	// decision works them out after the event's own change (Records.md ›
	// Built in slice 24).
	Computed map[string]json.RawMessage `json:"computed,omitempty"`
	// Meta are metadata fields (createdAt, updatedAt, createdBy) that take
	// the value of the record's own data (field id → _createdAt …) once the
	// event is folded.
	Meta map[string]string `json:"meta,omitempty"`
}

// Record is a record as projected: its values by field id, and its own
// data under keys starting with '_' (field ids never do).
type Record map[string]json.RawMessage

// Meta are the keys a record keeps its own data under.
const (
	MetaID        = "_id"
	MetaEntity    = "_entity"
	MetaSeq       = "_seq" // the last event's stream sequence, which record.update's expect names
	MetaCreatedAt = "_createdAt"
	MetaUpdatedAt = "_updatedAt"
	MetaCreatedBy = "_createdBy"
	MetaUpdatedBy = "_updatedBy"
	MetaProblems  = "_problems" // field id → why a conversion cleared its value
)

// Values are the record's field values, without its own data.
func (r Record) Values() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range r {
		if k == "" || k[0] != '_' {
			out[k] = v
		}
	}
	return out
}

// Seq is the record's last event's sequence.
func (r Record) Seq() uint64 {
	var n uint64
	_ = json.Unmarshal(r[MetaSeq], &n)
	return n
}

// Key is a record's key in the projection.
func Key(entity, id string) string { return entity + "." + id }

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// same reports whether two stored values are equal.
func same(a, b json.RawMessage) bool {
	if records.IsNull(a) || records.IsNull(b) {
		return records.IsNull(a) == records.IsNull(b)
	}
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// Apply folds one event into a record; nil is a deleted record.
func Apply(r Record, e live.Event) (Record, error) {
	var c Change
	if len(e.Data) > 0 {
		if err := json.Unmarshal(e.Data, &c); err != nil {
			return nil, err
		}
	}
	at := raw(e.Time.UTC().Format(time.RFC3339Nano))
	by := raw(e.UserID)
	switch e.Type {
	case EventCreated:
		r = Record{MetaID: raw(e.Target.ID), MetaEntity: raw(e.Target.Type), MetaCreatedAt: at, MetaCreatedBy: by}
		for k, v := range c.Values {
			if !records.IsNull(v) {
				r[k] = v
			}
		}
	case EventDeleted:
		return nil, nil
	case EventRecomputed:
		if r == nil {
			return nil, nil
		}
	case EventUpdated, EventConverted:
		if r == nil {
			return nil, nil // changes to a record that is gone
		}
		problems := map[string]string{}
		_ = json.Unmarshal(r[MetaProblems], &problems)
		for k, v := range c.Values {
			if records.IsNull(v) {
				delete(r, k)
			} else {
				r[k] = v
			}
			delete(problems, k)
		}
		for k, p := range c.Problems {
			problems[k] = p
		}
		if len(problems) == 0 {
			delete(r, MetaProblems)
		} else {
			r[MetaProblems] = raw(problems)
		}
	case EventAdded, EventRemoved:
		if r == nil {
			return nil, nil
		}
		var items []json.RawMessage
		_ = json.Unmarshal(r[c.Field], &items)
		for _, it := range c.Items {
			i := slices.IndexFunc(items, func(x json.RawMessage) bool { return same(x, it) })
			switch {
			case e.Type == EventAdded && i < 0:
				items = append(items, it)
			case e.Type == EventRemoved && i >= 0:
				items = slices.Delete(items, i, i+1)
			}
		}
		if len(items) == 0 {
			delete(r, c.Field)
		} else {
			r[c.Field] = raw(items)
		}
	default:
		return r, nil
	}
	r[MetaSeq] = raw(e.Seq)
	if e.Type != EventRecomputed {
		r[MetaUpdatedAt], r[MetaUpdatedBy] = at, by
	}
	for k, v := range c.Computed {
		if records.IsNull(v) {
			delete(r, k)
		} else {
			r[k] = v
		}
	}
	for k, m := range c.Meta {
		v, ok := r[m]
		if !ok {
			continue
		}
		if m == MetaCreatedBy || m == MetaUpdatedBy {
			// A user is named by their id as text, as a user field holds it
			var n uint64
			if json.Unmarshal(v, &n) == nil {
				v = raw(strconv.FormatUint(n, 10))
			}
		}
		r[k] = v
	}
	return r, nil
}

// fold is the records.record projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	if f.Event.Target.Type == CounterType {
		if f.Event.Type != EventCounted {
			return f.State, nil
		}
		var c Counter
		if err := json.Unmarshal(f.Event.Data, &c); err != nil {
			return nil, err
		}
		return json.Marshal(c)
	}
	var r Record
	if f.State != nil {
		if err := json.Unmarshal(f.State, &r); err != nil {
			return nil, err
		}
	}
	r, err := Apply(r, f.Event)
	if err != nil || r == nil {
		return nil, err
	}
	return json.Marshal(r)
}
