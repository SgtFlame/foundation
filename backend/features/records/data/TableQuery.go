package data

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// clause is a condition in SQL with its arguments. exact is false when it
// keeps more rows than the condition it stands for (or "" keeps all): the
// rows read are checked again in Go, so a clause need only never drop a
// row the condition keeps.
type clause struct {
	sql   string
	args  []any
	exact bool
}

func all(exact bool) clause { return clause{exact: exact} }

func and(cs ...clause) clause {
	out := clause{exact: true}
	var parts []string
	for _, c := range cs {
		out.exact = out.exact && c.exact
		if c.sql != "" {
			parts = append(parts, "("+c.sql+")")
			out.args = append(out.args, c.args...)
		}
	}
	out.sql = strings.Join(parts, " AND ")
	return out
}

func or(cs ...clause) clause {
	out := clause{exact: true}
	var parts []string
	for _, c := range cs {
		out.exact = out.exact && c.exact
		if c.sql == "" {
			return all(out.exact && c.exact)
		}
		parts = append(parts, "("+c.sql+")")
		out.args = append(out.args, c.args...)
	}
	out.sql = strings.Join(parts, " OR ")
	return out
}

// valueArg is a value as its v column holds it.
func valueArg(c col, v json.RawMessage) any {
	if c.idCol {
		var s string
		_ = json.Unmarshal(v, &s)
		return s
	}
	return jsonText(v)
}

// condSQL is one compiled condition in SQL, never NULL, so NOT applies.
func (s *tableSchema) condSQL(c *cond) clause {
	k, ok := s.col(c.field.ID)
	if !ok {
		return all(false)
	}
	v := fmt.Sprintf("r.%q", k.v)
	_, many := records.ItemOf(c.field)
	switch c.op {
	case "eq":
		if c.value == nil {
			return clause{sql: v + " IS NULL", exact: true}
		}
		return clause{sql: v + " IS NOT NULL AND " + v + " = ?", args: []any{valueArg(k, c.value)}, exact: true}
	case "ne":
		if c.value == nil {
			return clause{sql: v + " IS NOT NULL", exact: true}
		}
		return clause{sql: v + " IS NULL OR " + v + " <> ?", args: []any{valueArg(k, c.value)}, exact: true}
	case "in":
		var args []any
		null := false
		for _, x := range c.values {
			if x == nil {
				null = true
			} else {
				args = append(args, valueArg(k, x))
			}
		}
		parts := []string{}
		if len(args) > 0 {
			parts = append(parts, v+" IS NOT NULL AND "+v+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+")")
		}
		if null {
			parts = append(parts, v+" IS NULL")
		}
		if len(parts) == 0 {
			return clause{sql: "1 = 0", exact: true}
		}
		return clause{sql: "(" + strings.Join(parts, ") OR (") + ")", args: args, exact: true}
	case "lt", "lte", "gt", "gte":
		if k.t == "" {
			return all(false)
		}
		t := fmt.Sprintf("r.%q", k.t)
		op := map[string]string{"lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[c.op]
		return clause{sql: v + " IS NOT NULL AND " + t + " <> ? AND " + t + " " + op + " ?", args: []any{badToken, c.token}, exact: true}
	case "prefix":
		if k.t == "" || many {
			return all(false)
		}
		t := fmt.Sprintf("r.%q", k.t)
		// tokens begin with the text folded to lower case and cut at 120
		// bytes: a short prefix's range is exactly the texts it starts
		exact := len(c.text) <= 116 && !strings.ContainsRune(c.text, 0)
		return clause{sql: v + " IS NOT NULL AND " + t + " >= ? AND " + t + " < ?", args: []any{c.from, c.to}, exact: exact}
	case "contains":
		if many && k.child != "" {
			return clause{sql: fmt.Sprintf("EXISTS (SELECT 1 FROM %q c WHERE c.rid = r.id AND c.v = ?)", k.child), args: []any{jsonText(c.value)}, exact: true}
		}
		return all(false) // text containing, in any case: checked in Go
	}
	return all(false)
}

// nodeSQL is a compiled filter in SQL.
func (s *tableSchema) nodeSQL(n node) clause {
	switch {
	case n.c != nil:
		return s.condSQL(n.c)
	case n.or != nil:
		var cs []clause
		for _, m := range n.or {
			cs = append(cs, s.nodeSQL(m))
		}
		return or(cs...)
	}
	var cs []clause
	for _, m := range n.and {
		cs = append(cs, s.nodeSQL(m))
	}
	return and(cs...)
}

// ruleSQL is the principal's read rule in SQL, as far as it compiles: ==,
// !=, in, and, or and not over record fields, constants and the
// principal's user, tenant and kind, and ranges over number fields. What
// does not compile keeps every row, and the rule is applied in Go.
func (p *planned) ruleSQL(s *tableSchema) clause {
	if p.rule == nil || p.who.Kind == features.PrincipalService || p.ruleSrc == "" {
		return all(true)
	}
	tree, err := parser.Parse(p.ruleSrc)
	if err != nil {
		return all(false)
	}
	return ruleNode(p, s, tree.Node)
}

// RuleCompiles reports whether a row rule compiles to SQL in full over a
// type's table, for forge.check's warning; principal values are stand-ins.
func RuleCompiles(e records.EntityType, src string) bool {
	if src == "" {
		return true
	}
	tree, err := parser.Parse(src)
	if err != nil {
		return false
	}
	s := schemaFor(e, nil)
	p := &planned{e: e, who: features.Principal{Kind: features.PrincipalUser, UserID: 1, TenantID: 1}}
	return ruleNode(p, s, tree.Node).exact
}

func ruleNode(p *planned, s *tableSchema, n ast.Node) clause {
	switch x := n.(type) {
	case *ast.BinaryNode:
		switch x.Operator {
		case "&&", "and":
			return and(ruleNode(p, s, x.Left), ruleNode(p, s, x.Right))
		case "||", "or":
			return or(ruleNode(p, s, x.Left), ruleNode(p, s, x.Right))
		case "==", "!=", "<", "<=", ">", ">=":
			field, value, flipped := operands(p, x.Left, x.Right)
			if field == "" {
				return all(false)
			}
			op := map[string]string{"==": "eq", "!=": "ne", "<": "lt", "<=": "lte", ">": "gt", ">=": "gte"}[x.Operator]
			if flipped {
				op = map[string]string{"eq": "eq", "ne": "ne", "lt": "gt", "lte": "gte", "gt": "lt", "gte": "lte"}[op]
			}
			return ruleCond(p, s, Filter{Field: field, Op: op, Value: value}, value)
		case "in":
			if f, ok := recordField(x.Right); ok { // "a" in record.tags
				v, ok := constant(p, x.Left)
				if !ok {
					return all(false)
				}
				return ruleCond(p, s, Filter{Field: f, Op: "contains", Value: v}, v)
			}
			f, ok := recordField(x.Left)
			arr, isArr := x.Right.(*ast.ArrayNode)
			if !ok || !isArr {
				return all(false)
			}
			var vs []json.RawMessage
			for _, it := range arr.Nodes {
				v, ok := constant(p, it)
				if !ok {
					return all(false)
				}
				vs = append(vs, v)
			}
			list, _ := json.Marshal(vs)
			c := ruleCond(p, s, Filter{Field: f, Op: "in", Value: list}, nil)
			for _, v := range vs { // exact only when no value was normalized
				c.exact = c.exact && unchanged(p, f, v)
			}
			return c
		}
	case *ast.UnaryNode:
		if x.Operator == "!" || x.Operator == "not" {
			c := ruleNode(p, s, x.Node)
			if !c.exact || c.sql == "" {
				return all(false)
			}
			return clause{sql: "NOT (" + c.sql + ")", args: c.args, exact: true}
		}
	case *ast.BoolNode:
		if x.Value {
			return all(true)
		}
		return clause{sql: "1 = 0", exact: true}
	}
	return all(false)
}

// ruleCond compiles one comparison of a rule as a filter condition; it is
// exact when the filter compares as expr does: the value is kept as given,
// and a range is over a number field.
func ruleCond(p *planned, s *tableSchema, f Filter, value json.RawMessage) clause {
	n, err := compile(p.e, f)
	if err != nil || n.c == nil {
		return all(false)
	}
	c := s.condSQL(n.c)
	switch f.Op {
	case "lt", "lte", "gt", "gte":
		// expr fails on a missing value, which fails the whole rule
		c.exact = false
		if records.ValueField(n.c.field).Type != "number" {
			return all(false)
		}
	case "eq", "ne":
		c.exact = c.exact && unchanged(p, f.Field, value)
	case "contains":
		item, _ := records.ItemOf(n.c.field)
		v, err := records.CheckValue(item, value)
		c.exact = c.exact && err == nil && same(v, value)
	}
	return c
}

// unchanged reports whether the field keeps a value as written.
func unchanged(p *planned, field string, v json.RawMessage) bool {
	if records.IsNull(v) {
		return true
	}
	f, err := fieldOf(p.e, field)
	if err != nil {
		return false
	}
	if _, meta := metaFields[field]; meta {
		return true
	}
	out, err := records.CheckValue(f, v)
	return err == nil && same(out, v)
}

// operands are a comparison's record field and the value it is compared
// to; flipped when the field is on the right.
func operands(p *planned, l, r ast.Node) (string, json.RawMessage, bool) {
	if f, ok := recordField(l); ok {
		if v, ok := constant(p, r); ok {
			return f, v, false
		}
	}
	if f, ok := recordField(r); ok {
		if v, ok := constant(p, l); ok {
			return f, v, true
		}
	}
	return "", nil, false
}

// recordField is record.<field> (or record["field"]).
func recordField(n ast.Node) (string, bool) {
	m, ok := n.(*ast.MemberNode)
	if !ok {
		return "", false
	}
	id, ok := m.Node.(*ast.IdentifierNode)
	prop, isStr := m.Property.(*ast.StringNode)
	if !ok || !isStr || id.Value != "record" {
		return "", false
	}
	return prop.Value, true
}

// constant is a literal or one of the principal's values, as JSON.
func constant(p *planned, n ast.Node) (json.RawMessage, bool) {
	switch x := n.(type) {
	case *ast.StringNode:
		return raw(x.Value), true
	case *ast.IntegerNode:
		return raw(x.Value), true
	case *ast.FloatNode:
		return raw(x.Value), true
	case *ast.BoolNode:
		return raw(x.Value), true
	case *ast.NilNode:
		return json.RawMessage("null"), true
	case *ast.MemberNode:
		id, ok := x.Node.(*ast.IdentifierNode)
		prop, isStr := x.Property.(*ast.StringNode)
		if !ok || !isStr || id.Value != "principal" {
			return nil, false
		}
		env := records.RuleEnv(nil, p.who)["principal"].(map[string]any)
		switch prop.Value {
		case "user", "tenant", "kind", "app", "agent", "onBehalfOf":
			return raw(env[prop.Value]), true
		}
	}
	return nil, false
}

// ready brings a type's table up to date for a read: following, at its
// published version, and holding every event of the type committed before
// the read began, so a read sees what a command just wrote.
func (t *Tables) ready(args features.ServiceArgs, e records.EntityType) (*tableSchema, string, error) {
	ctx, tenant := args.Context(), args.Principal.TenantID
	if err := t.follow(t.ctx(), tenant, e.ID); err != nil {
		return nil, "", err
	}
	st, err := t.ensure(ctx, tenant, e)
	if err != nil {
		return nil, "", err
	}
	last, err := t.lastSeq(ctx, tenant, e.ID)
	if err != nil {
		return nil, "", err
	}
	limit := t.CatchUp
	if limit <= 0 {
		limit = 5 * time.Second
	}
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for st.Seq < last {
		changed := t.wait()
		var found bool
		if st, found, err = t.state(t.db, tenant, e.ID); err != nil || !found {
			return nil, "", log.Wrap(err)
		}
		if st.Seq >= last {
			break
		}
		select {
		case <-changed:
		case <-time.After(20 * time.Millisecond): // another process may write it
		case <-deadline.C:
			return nil, "", log.Code("table_behind").With("entity", e.ID).Errorf("the table of %s is behind its records (at %d of %d)", e.ID, st.Seq, last)
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	s, err := t.schema(st, e)
	return s, st.Name, err
}

// sqlRead is planned.read for a type with a table: the rows after the
// cursor in order, read in pages from SQL by the sort's tokens, each
// checked against the filter and rule in Go.
func (t *Tables) read(args features.ServiceArgs, p *planned, limit int) (Result, []row, error) {
	s, name, err := t.ready(args, p.e)
	if err != nil {
		return Result{}, nil, err
	}
	where := and(s.nodeSQL(p.filter), p.ruleSQL(s))
	res := Result{Plan: Plan{Index: "sql", Ordered: p.q.Limit > 0, Complete: true}, Pos: live.Position{}}
	var cols []string
	var desc []bool
	for i, f := range p.sort.fields {
		c, _ := s.col(f.ID)
		cols, desc = append(cols, c.t), append(desc, p.sort.desc[i])
	}
	last := len(desc) > 0 && desc[len(desc)-1]
	cols, desc = append(cols, "ord"), append(desc, last)
	order := make([]string, len(cols))
	for i, c := range cols {
		order[i] = fmt.Sprintf("r.%q", c)
		if desc[i] {
			order[i] += " DESC"
		}
	}
	batch := 200
	if p.q.Limit > 0 {
		batch = p.q.Limit + 1
		if !where.exact {
			batch = max(2*batch, 200)
		}
	}
	var after []any // the last row read: its tokens and ord
	if p.after != nil {
		for _, k := range p.after.Keys {
			after = append(after, k)
		}
		after = append(after, ordOf(p.after.ID))
	}
	var rows []row
	for {
		q := and(where, keyset(cols, desc, after))
		sql := fmt.Sprintf("SELECT r.id, r.data FROM %q r", name)
		if q.sql != "" {
			sql += " WHERE " + q.sql
		}
		sql += " ORDER BY " + strings.Join(order, ", ") + fmt.Sprintf(" LIMIT %d", batch)
		var got []struct {
			ID   string
			Data string
		}
		if err := t.db.WithContext(args.Context()).Raw(sql, q.args...).Scan(&got).Error; err != nil {
			return Result{}, nil, log.Wrapf(err, "cannot read the table of %s", p.e.ID)
		}
		for _, g := range got {
			if res.Plan.Read >= limit {
				res.Plan.Complete = false
				return res, rows, nil
			}
			res.Plan.Read++
			var r Record
			if json.Unmarshal([]byte(g.Data), &r) != nil {
				continue
			}
			rw := p.sort.row(r)
			after = append(after[:0], anys(rw.keys)...)
			after = append(after, ordOf(g.ID))
			if !p.match(r) {
				continue
			}
			res.Pos = res.Pos.Merge(live.Position{live.StreamName(args.Principal.TenantID, live.AreaRecords): r.Seq()})
			rows = append(rows, rw)
			if p.q.Limit > 0 && len(rows) > p.q.Limit {
				return res, rows, nil
			}
		}
		if len(got) < batch {
			return res, rows, nil
		}
	}
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// keyset is the rows after a position in the order cols and desc give.
func keyset(cols []string, desc []bool, after []any) clause {
	if after == nil {
		return all(true)
	}
	q := func(c string) string { return fmt.Sprintf("r.%q", c) }
	same := true
	for _, d := range desc {
		same = same && d == desc[0]
	}
	if same { // a row value, which an index answers as a range
		op := ">"
		if desc[0] {
			op = "<"
		}
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = q(c)
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
		return clause{sql: "(" + strings.Join(names, ", ") + ") " + op + " (" + marks + ")", args: after, exact: true}
	}
	var parts []string
	var args []any
	for i := range cols {
		var p []string
		for j := range i {
			p = append(p, q(cols[j])+" = ?")
			args = append(args, after[j])
		}
		op := ">"
		if desc[i] {
			op = "<"
		}
		p = append(p, q(cols[i])+" "+op+" ?")
		args = append(args, after[i])
		parts = append(parts, strings.Join(p, " AND "))
	}
	return clause{sql: "(" + strings.Join(parts, ") OR (") + ")", args: args, exact: true}
}

// summarize computes an aggregation in SQL when its filter and rule compile
// exactly; ok is false when it must read the rows instead.
func (t *Tables) summarize(args features.ServiceArgs, a *aggregation) (Summary, bool, error) {
	p := a.p
	s, name, err := t.ready(args, p.e)
	if err != nil {
		return Summary{}, false, err
	}
	where := and(s.nodeSQL(p.filter), p.ruleSQL(s))
	if !where.exact {
		return Summary{}, false, nil
	}
	from := fmt.Sprintf("%q r", name)
	groupTok, groupVal := "''", "NULL"
	var keyCol col // a scalar group's columns: its keys are read after
	if a.group != nil {
		g, ok := s.col(a.group.ID)
		switch {
		case !ok:
			return Summary{}, false, nil
		case g.child != "":
			from += fmt.Sprintf(" LEFT JOIN %q c ON c.rid = r.id", g.child)
			groupTok, groupVal = fmt.Sprintf("COALESCE(c.t, '%s')", records.NullToken), "MIN(c.v)"
		default:
			// counting over the index alone; each group's key is read after
			groupTok, keyCol = fmt.Sprintf("r.%q", g.t), g
		}
	}
	selects := []string{groupTok + " AS g", groupVal + " AS k"}
	type minmax struct {
		i   int
		col col
	}
	var mm []minmax
	for i, c := range a.aggs {
		k, _ := s.col(c.field.ID)
		switch {
		case c.Field == "":
			selects = append(selects, "COUNT(*)", "0", "0")
		case c.Op == "count":
			selects = append(selects, fmt.Sprintf("COUNT(r.%q)", k.v), "0", "0")
		case c.Op == "sum" || c.Op == "avg":
			if k.n == "" {
				return Summary{}, false, nil
			}
			selects = append(selects, fmt.Sprintf("COUNT(r.%q)", k.n), fmt.Sprintf("SUM(r.%q)", k.n), fmt.Sprintf("COUNT(r.%q)", k.v))
		default: // min, max: the best token; its value is looked up after
			if k.t == "" {
				return Summary{}, false, nil
			}
			fn := strings.ToUpper(c.Op)
			selects = append(selects, fmt.Sprintf("%s(CASE WHEN r.%q IS NOT NULL AND r.%q <> '%s' THEN r.%q END)", fn, k.v, k.t, badToken, k.t), "0", "0")
			mm = append(mm, minmax{i, k})
		}
	}
	sql := "SELECT " + strings.Join(selects, ", ") + " FROM " + from
	if where.sql != "" {
		sql += " WHERE " + where.sql
	}
	if a.group != nil {
		sql += " GROUP BY g ORDER BY g"
	}
	rows, err := t.db.WithContext(args.Context()).Raw(sql, where.args...).Rows()
	if err != nil {
		log.With("entity", p.e.ID).Warn(args.Context(), "an aggregate falls back to reading rows: %v", err)
		return Summary{}, false, nil
	}
	defer rows.Close()
	out := Summary{Groups: []Group{}, Plan: Plan{Index: "sql", Complete: true}}
	type best struct {
		group int
		agg   int
		token string
	}
	var bests []best
	accs := [][]*accumulator{}
	for rows.Next() {
		var g string
		var k *string
		cells := make([]any, 3*len(a.aggs))
		for i := range cells {
			cells[i] = new(any)
		}
		if err := rows.Scan(append([]any{&g, &k}, cells...)...); err != nil {
			return Summary{}, false, log.Wrap(err)
		}
		grp := Group{Key: json.RawMessage("null"), Values: map[string]json.RawMessage{}, token: g}
		if k != nil {
			grp.Key = json.RawMessage(*k)
		}
		var xs []*accumulator
		for i, c := range a.aggs {
			x := &accumulator{c: c, sum: new(big.Rat)}
			n, sum, values := *(cells[3*i].(*any)), *(cells[3*i+1].(*any)), *(cells[3*i+2].(*any))
			switch {
			case c.Op == "min" || c.Op == "max":
				if tok, ok := text(n); ok {
					bests = append(bests, best{len(out.Groups), i, tok})
				}
			case c.Op == "sum" || c.Op == "avg":
				x.n = int(integer(n))
				if x.n != int(integer(values)) {
					return Summary{}, false, nil // a value its column could not hold
				}
				if records.ValueField(c.field).Type == "number" {
					x.fsum = float(sum)
				} else {
					x.sum.SetFrac(big.NewInt(integer(sum)), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scaleOf(records.ValueField(c.field)))), nil))
				}
			default:
				x.n = int(integer(n))
			}
			xs = append(xs, x)
		}
		accs = append(accs, xs)
		out.Groups = append(out.Groups, grp)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, false, log.Wrap(err)
	}
	if keyCol.t != "" {
		if err := t.groupKeys(args, name, keyCol, out.Groups); err != nil {
			return Summary{}, false, err
		}
	}
	// min and max: the values their tokens are of
	for _, m := range mm {
		var toks []any
		for _, b := range bests {
			if b.agg == m.i {
				toks = append(toks, b.token)
			}
		}
		if len(toks) == 0 {
			continue
		}
		var found []struct {
			T string
			V string
		}
		q := fmt.Sprintf(`SELECT r.%q AS t, MIN(r.%q) AS v FROM %q r WHERE r.%q IN (%s) GROUP BY r.%q`, m.col.t, m.col.v, name, m.col.t, strings.TrimSuffix(strings.Repeat("?,", len(toks)), ","), m.col.t)
		if err := t.db.WithContext(args.Context()).Raw(q, toks...).Scan(&found).Error; err != nil {
			return Summary{}, false, log.Wrap(err)
		}
		values := map[string]string{}
		for _, f := range found {
			values[f.T] = f.V
		}
		for _, b := range bests {
			if b.agg == m.i {
				x := accs[b.group][b.agg]
				x.best, x.bestTok, x.hasValue = json.RawMessage(values[b.token]), b.token, true
			}
		}
	}
	for gi, g := range out.Groups {
		for _, x := range accs[gi] {
			g.Values[x.c.As] = x.value()
		}
	}
	return out, true, nil
}

// groupKeys reads each group's key, the value of a row with its token:
// one lookup by the index per group, or one grouped read when there are
// many.
func (t *Tables) groupKeys(args features.ServiceArgs, name string, k col, groups []Group) error {
	db := t.db.WithContext(args.Context())
	if len(groups) > 256 {
		var found []struct {
			T string
			V *string
		}
		q := fmt.Sprintf(`SELECT r.%q AS t, MIN(r.%q) AS v FROM %q r GROUP BY r.%q`, k.t, k.v, name, k.t)
		if err := db.Raw(q).Scan(&found).Error; err != nil {
			return log.Wrap(err)
		}
		keys := map[string]*string{}
		for _, f := range found {
			keys[f.T] = f.V
		}
		for i := range groups {
			if v := keys[groups[i].token]; v != nil {
				groups[i].Key = json.RawMessage(*v)
			}
		}
		return nil
	}
	for i := range groups {
		if groups[i].token == records.NullToken {
			continue
		}
		var v []*string
		q := fmt.Sprintf(`SELECT r.%q FROM %q r WHERE r.%q = ? LIMIT 1`, k.v, name, k.t)
		if err := db.Raw(q, groups[i].token).Scan(&v).Error; err != nil {
			return log.Wrap(err)
		}
		if len(v) > 0 && v[0] != nil {
			groups[i].Key = json.RawMessage(*v[0])
		}
	}
	return nil
}

func text(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

func integer(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case []byte:
		var n int64
		_, _ = fmt.Sscan(string(x), &n)
		return n
	}
	return 0
}

func float(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}
