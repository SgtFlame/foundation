package data

import (
	"encoding/json"
	"math/big"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Aggregation is a read of aggregates over an entity type's records
// (records.aggregate; Records.md › Built in slice 20): the records where
// and filter keep, grouped by a field, and each aggregate per group.
type Aggregation struct {
	Entity string                     `json:"entity"`
	Where  map[string]json.RawMessage `json:"where,omitempty"`
	Filter *Filter                    `json:"filter,omitempty"`
	// GroupBy is the field to group by; one group of every record when
	// empty. A list-like field puts a record in a group per item it holds.
	GroupBy    string      `json:"groupBy,omitempty"`
	Aggregates []Aggregate `json:"aggregates"`
}

// Aggregate is one aggregate: count (records, or with a field those that
// have a value), sum and avg over a number, decimal or money field with a
// currency, and min and max over a field with an order.
type Aggregate struct {
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
	As    string `json:"as,omitempty"` // its name in a group's values; op_field (or count) when left out
}

// AggregateOps are an aggregate's operations.
var AggregateOps = records.Aggregates

// Summary is an aggregation's result.
type Summary struct {
	Groups []Group `json:"groups"` // ordered by key, as a sort by the field orders them
	Plan   Plan    `json:"plan"`
}

// Group is one group's key and aggregates. A sum over no values is 0, and
// avg, min and max over none are null. A decimal's sum keeps the field's
// scale and its avg two digits more; a number's are numbers.
type Group struct {
	Key    json.RawMessage            `json:"key"` // the group's value; null for records without one
	Values map[string]json.RawMessage `json:"values"`
	token  string
}

// GroupsName is the list a records.aggregate result's changes apply to.
const GroupsName = "groups"

// GroupKey is a group's key in its changes: the key's JSON.
func GroupKey(key json.RawMessage) string {
	if records.IsNull(key) {
		return "null"
	}
	return string(key)
}

// aggregation is an aggregation ready to run.
type aggregation struct {
	p     *planned
	group *records.Field
	aggs  []compiledAgg
}

type compiledAgg struct {
	Aggregate
	field records.Field
}

func prepareAggregation(args features.ServiceArgs, d Deps, q Aggregation) (*aggregation, error) {
	p, err := prepare(args, d, Query{Entity: q.Entity, Where: q.Where, Filter: q.Filter})
	if err != nil {
		return nil, err
	}
	a := &aggregation{p: p}
	bad := func(format string, args ...any) (*aggregation, error) {
		return nil, log.Code("bad_aggregate").Errorf(format, args...)
	}
	if q.GroupBy != "" {
		f, err := fieldOf(p.e, q.GroupBy)
		if err != nil {
			return bad("%s has no field %q to group by", p.e.Name, q.GroupBy)
		}
		if item, _ := records.ItemOf(f); !records.Sortable(item) {
			return bad("%s is a %s field, whose values cannot be grouped", f.Name, f.Type)
		}
		a.group = &f
	}
	if len(q.Aggregates) == 0 {
		return bad("give at least one aggregate, such as {op: \"count\"}")
	}
	names := map[string]bool{}
	for _, g := range q.Aggregates {
		c := compiledAgg{Aggregate: g}
		if !slices.Contains(AggregateOps, g.Op) {
			return bad("there is no aggregate %q; use one of %s", g.Op, strings.Join(AggregateOps, ", "))
		}
		if g.Field != "" {
			if c.field, err = fieldOf(p.e, g.Field); err != nil {
				return bad("%s has no field %q", p.e.Name, g.Field)
			}
		} else if g.Op != "count" {
			return bad("%s needs a field", g.Op)
		}
		_, many := records.ItemOf(c.field)
		switch g.Op {
		case "sum", "avg":
			if t := c.field.Type; t != "number" && t != "decimal" && !(t == "money" && c.field.Currency != "") {
				return bad("%s %s: a %s field cannot be summed; use a number, decimal or money field with a currency", g.Op, g.Field, t)
			}
		case "min", "max":
			if many || !records.Sortable(c.field) {
				return bad("%s %s: a %s field has no order", g.Op, g.Field, c.field.Type)
			}
		}
		if c.As == "" {
			c.As = g.Op
			if g.Field != "" {
				c.As += "_" + g.Field
			}
		}
		if names[c.As] {
			return bad("two aggregates are named %q; give one another as", c.As)
		}
		names[c.As] = true
		a.aggs = append(a.aggs, c)
	}
	return a, nil
}

// Summarize computes an aggregation once, from every record it keeps.
func Summarize(args features.ServiceArgs, d Deps, q Aggregation) (Summary, error) {
	a, err := prepareAggregation(args, d, q)
	if err != nil {
		return Summary{}, err
	}
	return a.run(args, d)
}

func (a *aggregation) run(args features.ServiceArgs, d Deps) (Summary, error) {
	res, rows, err := a.p.read(args, d)
	if err != nil {
		return Summary{}, err
	}
	if !res.Plan.Complete {
		return Summary{}, log.Code("scan_limit").With("entity", a.p.e.ID).Errorf("%s has more than %d records, more than an aggregate reads without an index", a.p.e.ID, d.settings(args).ScanLimit)
	}
	type acc struct {
		key   json.RawMessage
		token string
		accs  []*accumulator
	}
	groups := map[string]*acc{}
	add := func(key json.RawMessage, token string, r Record) {
		g := groups[token]
		if g == nil {
			g = &acc{key: key, token: token}
			for _, c := range a.aggs {
				g.accs = append(g.accs, &accumulator{c: c, sum: new(big.Rat)})
			}
			groups[token] = g
		}
		for _, x := range g.accs {
			x.add(r)
		}
	}
	if a.group == nil {
		add(json.RawMessage("null"), "", nil) // the one group, even with no records
	}
	for _, row := range rows {
		r := row.record
		if a.group == nil {
			add(json.RawMessage("null"), "", r)
			continue
		}
		v := r[a.group.ID]
		item, many := records.ItemOf(*a.group)
		if !many || records.IsNull(v) {
			t, _ := records.SortToken(*a.group, v)
			add(orNull(v), t, r)
			continue
		}
		var items []json.RawMessage
		_ = json.Unmarshal(v, &items)
		if len(items) == 0 {
			add(json.RawMessage("null"), records.NullToken, r)
		}
		for _, it := range items {
			t, _ := records.SortToken(item, it)
			add(it, t, r)
		}
	}
	out := Summary{Groups: []Group{}, Plan: res.Plan}
	for _, g := range groups {
		grp := Group{Key: g.key, Values: map[string]json.RawMessage{}, token: g.token}
		for _, x := range g.accs {
			grp.Values[x.c.As] = x.value()
		}
		out.Groups = append(out.Groups, grp)
	}
	slices.SortFunc(out.Groups, func(x, y Group) int { return strings.Compare(x.token, y.token) })
	return out, nil
}

// accumulator is one aggregate over one group's records.
type accumulator struct {
	c        compiledAgg
	n        int      // records counted, or values summed
	sum      *big.Rat // decimal and money
	fsum     float64  // number
	best     json.RawMessage
	bestTok  string
	hasValue bool
}

func (x *accumulator) add(r Record) {
	if r == nil {
		return
	}
	if x.c.Field == "" { // count of records
		x.n++
		return
	}
	v := r[x.c.field.ID]
	if records.IsNull(v) {
		return
	}
	switch x.c.Op {
	case "count":
		x.n++
	case "sum", "avg":
		if x.c.field.Type == "number" {
			var f float64
			if json.Unmarshal(v, &f) != nil {
				return
			}
			x.fsum += f
		} else {
			var s string
			q, ok := new(big.Rat), json.Unmarshal(v, &s) == nil
			if !ok {
				return
			}
			if _, ok := q.SetString(s); !ok {
				return
			}
			x.sum.Add(x.sum, q)
		}
		x.n++
	case "min", "max":
		t, err := records.SortToken(x.c.field, v)
		if err != nil {
			return
		}
		if !x.hasValue || x.c.Op == "min" && t < x.bestTok || x.c.Op == "max" && t > x.bestTok {
			x.best, x.bestTok, x.hasValue = v, t, true
		}
	}
}

func (x *accumulator) value() json.RawMessage {
	scale := 0
	if x.c.field.Scale != nil {
		scale = *x.c.field.Scale
	}
	switch x.c.Op {
	case "count":
		return raw(x.n)
	case "sum":
		if x.c.field.Type == "number" {
			return raw(x.fsum)
		}
		return raw(x.sum.FloatString(scale))
	case "avg":
		if x.n == 0 {
			return json.RawMessage("null")
		}
		if x.c.field.Type == "number" {
			return raw(x.fsum / float64(x.n))
		}
		return raw(new(big.Rat).Quo(x.sum, big.NewRat(int64(x.n), 1)).FloatString(scale + 2))
	}
	if !x.hasValue {
		return json.RawMessage("null")
	}
	return x.best
}
