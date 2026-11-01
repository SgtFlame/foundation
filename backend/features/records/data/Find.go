package data

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/expr-lang/expr/vm"
)

// Query is a read of an entity type's records: records.list's snapshot,
// recordservice.Find and the records.query tool. The planner answers it
// from an index when one fits and scans otherwise, and says which.
type Query struct {
	Entity string `json:"entity"`
	// Where keeps the records whose fields equal these values (field id →
	// value, null for none); it is an and of eq conditions with Filter.
	Where  map[string]json.RawMessage `json:"where,omitempty"`
	Filter *Filter                    `json:"filter,omitempty"`
	Sort   []Sort                     `json:"sort,omitempty"`
	Limit  int                        `json:"limit,omitempty"`  // 0 for every record
	Cursor string                     `json:"cursor,omitempty"` // a result's next, for the following page
	Fields []string                   `json:"fields,omitempty"` // the fields to return; every field when empty
	// Expand follows these relation fields one level: each record gets the
	// records they point to under _expanded.<field> (Records.md › Built in
	// slice 20)
	Expand []string `json:"expand,omitempty"`
}

// Filter is a condition on a field, or an and or or of filters.
type Filter struct {
	And   []Filter        `json:"and,omitempty"`
	Or    []Filter        `json:"or,omitempty"`
	Field string          `json:"field,omitempty"` // a field id, or _id, _createdAt or _updatedAt
	Op    string          `json:"op,omitempty"`    // Ops; eq when left out
	Value json.RawMessage `json:"value,omitempty"`
}

// Ops are a filter's operators. A record without a value matches only eq
// null and ne a value. prefix and contains on text ignore case; contains
// on a list-like field is an item it holds. Ranges compare in sort order.
var Ops = []string{"eq", "ne", "lt", "lte", "gt", "gte", "in", "prefix", "contains"}

// Sort is one sort field. Ties are broken by record id (a shorter one
// first, as TSIDs are made: compareRecordIds), in the direction
// of the last sort field (ascending without a sort). A record without a
// value sorts first ascending, last descending.
type Sort struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc,omitempty"`
}

// Result is a query's page of records.
type Result struct {
	Records []Record      `json:"records"`
	Next    string        `json:"next,omitempty"` // the cursor of the page after; empty on the last
	Plan    Plan          `json:"plan"`
	Pos     live.Position `json:"-"`
}

// Plan is how a query was answered.
type Plan struct {
	Index   string `json:"index,omitempty"`   // the index used (several joined by '+', for an or)
	Ordered bool   `json:"ordered,omitempty"` // the index gave the order, so only the page was read
	Scan    bool   `json:"scan,omitempty"`    // every record of the type was read
	Read    int    `json:"read"`              // records read
	// Complete is false when a scan or an ordered walk stopped at the
	// records section's scanLimit: records past it were not looked at.
	Complete bool `json:"complete"`
}

// metaFields are the record's own data a query may filter and sort by.
var metaFields = map[string]records.Field{
	MetaID:        {ID: MetaID, Name: "id", Type: "relation"},
	MetaCreatedAt: {ID: MetaCreatedAt, Name: "created", Type: "dateTime"},
	MetaUpdatedAt: {ID: MetaUpdatedAt, Name: "updated", Type: "dateTime"},
}

// cond is one compiled condition, its values normalized for the field.
type cond struct {
	field  records.Field
	op     string
	value  json.RawMessage   // eq, ne, contains: nil is no value
	values []json.RawMessage // in
	text   string            // prefix, and contains on text: lower case
	token  string            // eq, contains on a list, ranges: the value's sort token
	tokens []string          // in
	from   string            // prefix: its range of tokens
	to     string
}

// node is a compiled filter.
type node struct {
	and, or []node
	c       *cond
}

func (n node) match(r Record) bool {
	switch {
	case n.c != nil:
		return n.c.match(r)
	case n.or != nil:
		return slices.ContainsFunc(n.or, func(m node) bool { return m.match(r) })
	}
	for _, m := range n.and {
		if !m.match(r) {
			return false
		}
	}
	return true
}

func (c *cond) match(r Record) bool {
	v := r[c.field.ID]
	switch c.op {
	case "eq":
		return c.eq(v, c.value)
	case "ne":
		return !c.eq(v, c.value)
	case "in":
		return slices.ContainsFunc(c.values, func(x json.RawMessage) bool { return c.eq(v, x) })
	case "prefix", "contains":
		if _, many := records.ItemOf(c.field); many {
			var items []json.RawMessage
			_ = json.Unmarshal(v, &items)
			return slices.ContainsFunc(items, func(x json.RawMessage) bool { return same(x, c.value) })
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return false
		}
		if c.op == "prefix" {
			return strings.HasPrefix(strings.ToLower(s), c.text)
		}
		return strings.Contains(strings.ToLower(s), c.text)
	}
	if records.IsNull(v) {
		return false
	}
	t, err := records.SortToken(c.field, v)
	if err != nil {
		return false
	}
	switch k := strings.Compare(t, c.token); c.op {
	case "lt":
		return k < 0
	case "lte":
		return k <= 0
	case "gt":
		return k > 0
	default: // gte
		return k >= 0
	}
}

func (c *cond) eq(v, want json.RawMessage) bool {
	if want == nil {
		return records.IsNull(v)
	}
	return same(v, want)
}

// fieldOf is a field a query names: a visible field, or a meta field.
func fieldOf(e records.EntityType, id string) (records.Field, error) {
	if f, ok := metaFields[id]; ok {
		return f, nil
	}
	f, _, ok := e.FieldByID(id)
	if !ok || f.Hidden {
		return f, log.Code("bad_filter").Errorf("%s has no field %q", e.Name, id)
	}
	return f, nil
}

func compile(e records.EntityType, f Filter) (node, error) {
	if len(f.And) > 0 || len(f.Or) > 0 {
		if f.Field != "" {
			return node{}, log.Code("bad_filter").Errorf("a filter is a condition or an and/or, not both")
		}
		n := node{}
		for _, g := range f.And {
			m, err := compile(e, g)
			if err != nil {
				return node{}, err
			}
			if m.c == nil && m.or == nil { // an and in an and is one and
				n.and = append(n.and, m.and...)
			} else {
				n.and = append(n.and, m)
			}
		}
		if len(f.Or) > 0 {
			or := node{or: []node{}}
			for _, g := range f.Or {
				m, err := compile(e, g)
				if err != nil {
					return node{}, err
				}
				or.or = append(or.or, m)
			}
			if len(n.and) == 0 {
				return or, nil
			}
			n.and = append(n.and, or)
		}
		if len(n.and) == 1 {
			return n.and[0], nil
		}
		return n, nil
	}
	if f.Field == "" {
		return node{}, log.Code("bad_filter").Errorf("a filter needs a field, and or or")
	}
	field, err := fieldOf(e, f.Field)
	if err != nil {
		return node{}, err
	}
	c := &cond{field: field, op: f.Op}
	if c.op == "" {
		c.op = "eq"
	}
	bad := func(format string, a ...any) (node, error) {
		return node{}, log.Code("bad_filter").Errorf("%s %s: %s", f.Field, c.op, fmt.Sprintf(format, a...))
	}
	item, many := records.ItemOf(field)
	check := func(f records.Field, v json.RawMessage) (json.RawMessage, error) {
		if _, meta := metaFields[f.ID]; meta && f.Type == "relation" {
			var s string
			if json.Unmarshal(v, &s) != nil || !live.ValidToken(s) {
				return nil, fmt.Errorf("%s is not a record id", v)
			}
			return v, nil
		}
		return records.CheckValue(f, v)
	}
	switch c.op {
	case "eq", "ne":
		if c.value, err = check(field, f.Value); err != nil {
			return bad("%v", err)
		}
		if records.Sortable(field) {
			c.token, _ = records.SortToken(field, c.value)
		}
	case "lt", "lte", "gt", "gte":
		if !records.Sortable(field) {
			return bad("%s values have no order", field.Type)
		}
		if records.IsNull(f.Value) {
			return bad("a range needs a value")
		}
		if c.value, err = check(field, f.Value); err != nil {
			return bad("%v", err)
		}
		c.token, _ = records.SortToken(field, c.value)
	case "in":
		var vs []json.RawMessage
		if json.Unmarshal(f.Value, &vs) != nil {
			return bad("in takes a list of values")
		}
		for _, v := range vs {
			x, err := check(field, v)
			if err != nil {
				return bad("%v", err)
			}
			c.values = append(c.values, x)
			if records.Sortable(field) {
				t, _ := records.SortToken(field, x)
				c.tokens = append(c.tokens, t)
			}
		}
	case "prefix", "contains":
		if many && c.op == "contains" {
			if c.value, err = check(item, f.Value); err != nil || c.value == nil {
				return bad("not an item of %s: %v", field.Name, err)
			}
			if records.Sortable(item) {
				c.token, _ = records.SortToken(item, c.value)
			}
			break
		}
		if field.Type != "text" && field.Type != "longText" {
			return bad("%s is not text", field.Name)
		}
		var s string
		if json.Unmarshal(f.Value, &s) != nil {
			return bad("%s takes text", c.op)
		}
		c.text = strings.ToLower(s)
		if c.op == "prefix" {
			c.from, c.to = records.PrefixRange(s)
		}
	default:
		return bad("no such operator; use one of %s", strings.Join(Ops, ", "))
	}
	return node{c: c}, nil
}

// sorter orders rows by the sort's tokens, then by record id.
type sorter struct {
	fields []records.Field
	desc   []bool
}

type row struct {
	id     string
	keys   []string
	record Record
}

func (s sorter) row(r Record) row {
	out := row{record: r}
	_ = json.Unmarshal(r[MetaID], &out.id)
	for _, f := range s.fields {
		t, err := records.SortToken(f, r[f.ID])
		if err != nil {
			t = records.Token([]byte{0xFF})
		}
		out.keys = append(out.keys, t)
	}
	return out
}

func (s sorter) compare(a, b row) int {
	for i := range s.fields {
		if k := strings.Compare(a.keys[i], b.keys[i]); k != 0 {
			if s.desc[i] {
				return -k
			}
			return k
		}
	}
	k := compareIDs(a.id, b.id)
	if len(s.desc) > 0 && s.desc[len(s.desc)-1] {
		return -k
	}
	return k
}

// cursor is where a page ended: the last row's sort tokens and id.
type cursor struct {
	Keys []string `json:"k"`
	ID   string   `json:"id"`
}

func (c cursor) String() string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func parseCursor(s string, n int) (*cursor, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	var c cursor
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	if err != nil || len(c.Keys) != n {
		return nil, log.Code("bad_cursor").Errorf("the cursor is not one this query gave")
	}
	return &c, nil
}

// planned is a query ready to run.
type planned struct {
	e      records.EntityType
	filter node
	sort   sorter
	after  *cursor
	q      Query
	// rule is the entity type's read rule, which hides records from who
	// as a filter does (Records.md › Built in slice 21)
	rule   *vm.Program
	who    features.Principal
	expand []expansion
}

// match reports whether a record is one the query gives its principal.
func (p *planned) match(r Record) bool {
	return p.filter.match(r) && records.Allows(p.rule, r, p.who)
}

func prepare(args features.ServiceArgs, d Deps, q Query) (*planned, error) {
	if !live.ValidToken(q.Entity) {
		return nil, log.Code("entity_not_found").Errorf("%q is not an entity type id", q.Entity)
	}
	e, problems := Entity(args, d.Projections, q.Entity)
	if problems != nil {
		return nil, log.Code("entity_not_found").Errorf("%s", problems[0].Message)
	}
	p := &planned{e: e, q: q, rule: e.Rule("read"), who: args.Principal}
	var and []Filter
	ids := make([]string, 0, len(q.Where))
	for id := range q.Where {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		and = append(and, Filter{Field: id, Op: "eq", Value: q.Where[id]})
	}
	if q.Filter != nil {
		and = append(and, *q.Filter)
	}
	var err error
	if len(and) > 0 {
		if p.filter, err = compile(e, Filter{And: and}); err != nil {
			return nil, err
		}
	}
	for _, s := range q.Sort {
		f, err := fieldOf(e, s.Field)
		if err != nil {
			return nil, log.Code("bad_sort").Errorf("%s has no field %q to sort by", e.Name, s.Field)
		}
		if !records.Sortable(f) {
			return nil, log.Code("bad_sort").Errorf("%s is a %s field, whose values have no order", f.Name, f.Type)
		}
		p.sort.fields = append(p.sort.fields, f)
		p.sort.desc = append(p.sort.desc, s.Desc)
	}
	if p.after, err = parseCursor(q.Cursor, len(p.sort.fields)); err != nil {
		return nil, err
	}
	if p.expand, err = expansions(args, d, e, q.Expand); err != nil {
		return nil, err
	}
	return p, nil
}

// conds are the conditions every match needs: the top-level and's.
func (n node) conds() []*cond {
	if n.c != nil {
		return []*cond{n.c}
	}
	var out []*cond
	for _, m := range n.and {
		if m.c != nil {
			out = append(out, m.c)
		}
	}
	return out
}

// path is one way through an index: [from, to) ranges of its entries.
type path struct {
	index   records.Index
	ranges  [][2]string
	score   int
	ordered bool
}

// pathFor is the best way through index x for conds, or nil.
func (p *planned) pathFor(x records.Index, conds []*cond) *path {
	prefixes := []string{""}
	k, ranged := 0, false
	var lo, hi []string // range bounds after the prefix
	for _, id := range x.Fields {
		var toks []string
		for _, c := range conds {
			if c.field.ID != id {
				continue
			}
			_, many := records.ItemOf(c.field)
			switch {
			case c.op == "eq" && !many && c.token != "":
				toks = []string{c.token}
			case c.op == "contains" && many && c.token != "":
				toks = []string{c.token}
			case c.op == "in" && !many && len(c.tokens) > 0 && toks == nil:
				toks = c.tokens
			}
		}
		if toks != nil && len(prefixes)*len(toks) <= 256 {
			next := []string{}
			for _, pre := range prefixes {
				for _, t := range toks {
					next = append(next, pre+t+".")
				}
			}
			prefixes = next
			k++
			continue
		}
		for _, c := range conds {
			if c.field.ID != id {
				continue
			}
			switch c.op {
			case "gt":
				lo = append(lo, c.token+"/")
			case "gte":
				lo = append(lo, c.token)
			case "lt":
				hi = append(hi, c.token)
			case "lte":
				hi = append(hi, c.token+"/")
			case "prefix":
				lo, hi = append(lo, c.from), append(hi, c.to)
			default:
				continue
			}
			ranged = true
		}
		break
	}
	if k == 0 && !ranged {
		return nil
	}
	pt := &path{index: x, score: 2 * k}
	if ranged {
		pt.score++
	}
	for _, pre := range prefixes {
		from, to := pre, ""
		if pre != "" {
			to = pre[:len(pre)-1] + "/" // every entry starting with pre
		}
		if ranged {
			from = max(from, pre+records.Token([]byte{1})) // values only, not none
			for _, l := range lo {
				from = max(from, pre+l)
			}
			for _, h := range hi {
				if to == "" || pre+h < to {
					to = pre + h
				}
			}
		}
		pt.ranges = append(pt.ranges, [2]string{from, to})
	}
	pt.ordered = len(prefixes) == 1 && p.sortIs(x.Fields[k:])
	return pt
}

// sortIs reports whether the sort is exactly these fields, in one
// direction, so an index's order is the sort's.
func (p *planned) sortIs(fields []string) bool {
	if len(p.sort.fields) == 0 || len(fields) != len(p.sort.fields) {
		return false
	}
	for i, f := range p.sort.fields {
		if f.ID != fields[i] || p.sort.desc[i] != p.sort.desc[0] {
			return false
		}
	}
	return true
}

// choose picks the best path through the type's indexes for conds.
func (p *planned) choose(conds []*cond) *path {
	var best *path
	for _, x := range p.e.Indexes {
		pt := p.pathFor(x, conds)
		if pt != nil && (best == nil || pt.score > best.score || pt.score == best.score && pt.ordered && !best.ordered) {
			best = pt
		}
	}
	return best
}

// Find answers a query: from an index walked in the sort's order when one
// fits, from an index's matches sorted in memory, or by a scan. Every
// record read is checked against the filter, so an index entry that is
// not current yet changes nothing.
func Find(args features.ServiceArgs, d Deps, q Query) (Result, error) {
	p, err := prepare(args, d, q)
	if err != nil {
		return Result{}, err
	}
	return p.run(args, d)
}

func (p *planned) run(args features.ServiceArgs, d Deps) (Result, error) {
	res, rows, err := p.read(args, d)
	if err != nil {
		return Result{}, err
	}
	res = p.page(res, rows)
	if err := p.expandAll(args, d, res.Records, rows); err != nil {
		return Result{}, err
	}
	return res, nil
}

// read gives the rows after the cursor, in order: for an ordered walk, the
// page and one more; otherwise every match. Result has no records yet.
func (p *planned) read(args features.ServiceArgs, d Deps) (Result, []row, error) {
	limit := d.settings(args).ScanLimit
	// An ordered walk: an index in the sort's order, the filter's path
	// through it or, without one, the whole index
	pt := p.choose(p.filter.conds())
	if pt == nil && p.filter.or == nil {
		for _, x := range p.e.Indexes {
			if p.sortIs(x.Fields) {
				pt = &path{index: x, ranges: [][2]string{{"", ""}}, ordered: true}
				break
			}
		}
	}
	if pt != nil && pt.ordered {
		return p.walk(args, d, pt, limit)
	}
	if pt != nil {
		return p.lookup(args, d, []*path{pt})
	}
	// An or of conditions each with a path is the union of their matches
	if p.filter.or != nil {
		var paths []*path
		for _, branch := range p.filter.or {
			bp := p.choose(branch.conds())
			if bp == nil {
				paths = nil
				break
			}
			paths = append(paths, bp)
		}
		if paths != nil {
			return p.lookup(args, d, paths)
		}
	}
	return p.scan(args, d, limit)
}

// get reads one record; nil when it is gone.
func get(args features.ServiceArgs, d Deps, entity, id string) (Record, live.Position, error) {
	v, ok, err := d.Projections.Get(args.Context(), args.Principal.TenantID, Projector, Key(entity, id))
	if err != nil || !ok {
		return nil, nil, err
	}
	var r Record
	if err := json.Unmarshal(v.Value, &r); err != nil {
		return nil, nil, err
	}
	return r, v.Position(), nil
}

// walk reads index entries in order, from the cursor, until the page is
// full. An entry whose record no longer has it is skipped: the record is
// found at its current entry.
func (p *planned) walk(args features.ServiceArgs, d Deps, pt *path, limit int) (Result, []row, error) {
	t, err := stateOf(d.Projections).index(args.Context(), args.Principal.TenantID, p.e.ID, pt.index.ID)
	if err != nil {
		return Result{}, nil, err
	}
	res := Result{Plan: Plan{Index: pt.index.ID, Ordered: true, Complete: true}, Pos: live.Position{}}
	desc := len(p.sort.desc) > 0 && p.sort.desc[0]
	from, to := pt.ranges[0][0], pt.ranges[0][1]
	if p.after != nil {
		// the cursor's entry: the prefix's tokens, the sort's, the id
		pre := ""
		if k := len(pt.index.Fields) - len(p.sort.fields); k > 0 {
			pre = strings.Join(strings.Split(from, ".")[:k], ".") + "."
		}
		at := pre + strings.Join(p.after.Keys, ".") + "." + EntryID(p.after.ID)
		if desc {
			if to == "" || at < to {
				to = at
			}
		} else {
			from = max(from, at+"\x00")
		}
	}
	var rows []row
	var failed error
	t.walk(from, to, desc, func(entry string) bool {
		if res.Plan.Read >= limit {
			res.Plan.Complete = false
			return false
		}
		toks, id := splitEntry(entry)
		r, pos, err := get(args, d, p.e.ID, id)
		if err != nil {
			failed = err
			return false
		}
		res.Plan.Read++
		if r == nil || !slices.Contains(EntryTokens(p.e, pt.index, r), toks) || !p.match(r) {
			return true
		}
		res.Pos = res.Pos.Merge(pos)
		rows = append(rows, p.sort.row(r))
		return p.q.Limit <= 0 || len(rows) <= p.q.Limit
	})
	if failed != nil {
		return Result{}, nil, log.Wrap(failed)
	}
	return res, rows, nil
}

// lookup reads the records the paths' entries name, then sorts them.
func (p *planned) lookup(args features.ServiceArgs, d Deps, paths []*path) (Result, []row, error) {
	res := Result{Plan: Plan{Complete: true}, Pos: live.Position{}}
	ids := map[string]bool{}
	var order []string
	var names []string
	for _, pt := range paths {
		t, err := stateOf(d.Projections).index(args.Context(), args.Principal.TenantID, p.e.ID, pt.index.ID)
		if err != nil {
			return Result{}, nil, err
		}
		if !slices.Contains(names, pt.index.ID) {
			names = append(names, pt.index.ID)
		}
		for _, r := range pt.ranges {
			t.walk(r[0], r[1], false, func(entry string) bool {
				_, id := splitEntry(entry)
				if !ids[id] {
					ids[id] = true
					order = append(order, id)
				}
				return true
			})
		}
	}
	res.Plan.Index = strings.Join(names, "+")
	var rows []row
	for _, id := range order {
		r, pos, err := get(args, d, p.e.ID, id)
		if err != nil {
			return Result{}, nil, log.Wrap(err)
		}
		res.Plan.Read++
		if r != nil && p.match(r) {
			res.Pos = res.Pos.Merge(pos)
			rows = append(rows, p.sort.row(r))
		}
	}
	return res, p.order(rows), nil
}

// scan reads every record of the type, up to the scan limit.
func (p *planned) scan(args features.ServiceArgs, d Deps, limit int) (Result, []row, error) {
	res := Result{Plan: Plan{Scan: true, Complete: true}, Pos: live.Position{}}
	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	entries, err := d.Projections.Watch(ctx, args.Principal.TenantID, Projector, p.e.ID+".*")
	if err != nil {
		return Result{}, nil, err
	}
	var rows []row
	for e := range entries {
		if e == nil {
			break
		}
		if e.Deleted {
			continue
		}
		if res.Plan.Read >= limit {
			res.Plan.Complete = false
			break
		}
		res.Plan.Read++
		var r Record
		if json.Unmarshal(e.Value.Value, &r) == nil && p.match(r) {
			res.Pos = res.Pos.Merge(e.Value.Position())
			rows = append(rows, p.sort.row(r))
		}
	}
	return res, p.order(rows), nil
}

// order sorts the rows and drops those up to the cursor.
func (p *planned) order(rows []row) []row {
	slices.SortFunc(rows, p.sort.compare)
	if p.after != nil {
		i, _ := slices.BinarySearchFunc(rows, p.at(), p.sort.compare)
		for i < len(rows) && p.sort.compare(rows[i], p.at()) <= 0 {
			i++
		}
		rows = rows[i:]
	}
	return rows
}

// at is the cursor as a row, which every row of the result comes after.
func (p *planned) at() row { return row{id: p.after.ID, keys: p.after.Keys} }

// page keeps a page of the rows, which are in order and after the cursor.
func (p *planned) page(res Result, rows []row) Result {
	if p.q.Limit > 0 && len(rows) > p.q.Limit {
		rows = rows[:p.q.Limit]
		last := rows[len(rows)-1]
		res.Next = cursor{Keys: last.keys, ID: last.id}.String()
	}
	res.Records = make([]Record, 0, len(rows))
	for _, r := range rows {
		res.Records = append(res.Records, p.fields(r.record))
	}
	return res
}

// fields keeps the fields the query asked for, and the record's own data.
func (p *planned) fields(r Record) Record {
	if len(p.q.Fields) == 0 {
		return r
	}
	out := Record{}
	for k, v := range r {
		if strings.HasPrefix(k, "_") || slices.Contains(p.q.Fields, k) {
			out[k] = v
		}
	}
	return out
}
