package ui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Windowed collections (forge 07l, documentation/ServerUI.md › Built in
// slice 53): mui.virtualList and mui.virtualGrid show an entity type's
// records, as many as there are, through a window the server holds. The
// browser virtualizes the scrolling and reports the rows it shows (the
// window event); the server holds records.find pages covering about three
// times that, a viewport above and below, and renders only those rows. So
// scrolling within the window costs no round trip, and the browser asks
// for a new one only when its rows near the window's edge.
//
// The two differ in what a row is on the wire. A virtualList's rows are
// its one child, a row template evaluated per record on the server (as a
// repeat's, the record under its as name), so the browser gets finished
// nodes. A virtualGrid's rows are the records themselves, as JSON, and
// the browser draws its columns' cells.
//
// Pages are records.find subscriptions of pageSize records, each after the
// cursor its previous page's next gave, so each stays current: a record
// another session changes updates in place. Cursors learned are kept in
// the window's state, so pages scrolled away are dropped and held again
// on return. A jump past the cursors known (the scrollbar dragged far)
// walks to it with a skip query of record ids, maxSkip rows at a time.
// The total, for the scrollbar, is a records.aggregate count.
const (
	VirtualListType = "mui.virtualList"
	VirtualGridType = "mui.virtualGrid"
	windowHandler   = "window:"
	windowState     = "_window_"
	defaultPage     = 50
	maxPage         = 500
	maxSkip         = 5000
)

// Windowed reports whether a component type is a windowed collection.
func Windowed(typ string) bool { return typ == VirtualListType || typ == VirtualGridType }

// windowParams are the props that make a window's query.
var windowParams = []string{"entity", "where", "filter", "sort", "fields", "expand"}

// window evaluates a windowed collection as a region of its own, so a new
// window or a page's change renders only it.
func (e *evaluator) window(r *Render, sc *scope, n *Node, path []string, key string) *Node {
	return r.Region(key, func(r *Render) *Node { return e.windowed(r, sc, n, path, key) })
}

func (e *evaluator) windowed(r *Render, sc *scope, n *Node, path []string, key string) *Node {
	s := e.s
	shell := *n
	shell.Children = nil
	out := e.primitive(r, sc, &shell, path, key)
	if out.Type != n.Type {
		return out // its problems
	}
	if sc.inst == nil {
		return ErrorNode(key, []Problem{{Message: n.Type + " is drawn only inside a prefab"}})
	}
	here := kpOf(append(slices.Clone(path), key))
	statePath := sc.inst.scoped(windowState + key)
	// The query: its props, bound ones read as bindings are
	params := map[string]any{}
	for _, p := range windowParams {
		if v := e.boundValue(r, out, p); v != nil {
			params[p] = v
		}
	}
	entity, _ := params["entity"].(string)
	if entity == "" {
		return ErrorNode(key, []Problem{{Message: n.Type + " needs an entity"}})
	}
	size := intProp(out.Props["pageSize"], defaultPage)
	size = min(max(size, 1), maxPage)
	// A new query starts over from the top
	base := jsonText(map[string]any{"q": params, "size": size})
	st, _ := getPath(s.state, statePath).(map[string]any)
	if st == nil || st["base"] != base {
		st = map[string]any{"base": base, "first": 0, "last": size - 1, "cursors": map[string]any{}}
		setPath(s.state, statePath, st)
	}
	r.State(statePath + ".first")
	r.State(statePath + ".last")
	first, last := intProp(st["first"], 0), intProp(st["last"], size-1)
	if last < first {
		last = first
	}
	cursors, _ := st["cursors"].(map[string]any)
	if cursors == nil {
		cursors = map[string]any{}
		st["cursors"] = cursors
	}
	cursor := func(k int) (string, bool) {
		if k == 0 {
			return "", true
		}
		c, ok := cursors[strconv.Itoa(k)].(string)
		return c, ok
	}
	// The window: a viewport above and below what is shown
	view := last - first + 1
	lo, hi := max(0, first-view), last+view
	kLo, kHi := lo/size, hi/size
	used := map[string]bool{}
	hold := func(name string, query string, p map[string]any) any {
		full := here + "#" + name
		used[full] = true
		ref := QueryRef{Query: s.page.Query(query), Params: p}
		if q := s.queries[full]; q == nil || q.ref.Query != ref.Query || string(s.boundParams(ref)) != string(q.params) {
			if prob := s.holdAs(sc.inst, full, ref, ""); prob != nil {
				return prob
			}
			q := s.queries[full]
			q.quiet, q.data = true, nil // not the last window's rows
		}
		return r.Query(full)
	}
	page := func(cur string, limit int, fields []string) map[string]any {
		p := maps.Clone(params)
		p["limit"] = limit
		if cur != "" {
			p["cursor"] = cur
		}
		if fields != nil {
			p["fields"] = fields
			delete(p, "expand")
		}
		return p
	}
	var problems []Problem
	var rows []any
	offset, more, loading := kLo*size, false, false
	seen := map[string]bool{}
	for k := kLo; k <= kHi; k++ {
		cur, ok := cursor(k)
		if !ok {
			// Walk to page k from the last page whose cursor is known
			known := 0
			for c := range cursors {
				if i, err := strconv.Atoi(c); err == nil && i < k && i > known {
					known = i
				}
			}
			from, _ := cursor(known)
			step := min((k-known)*size, maxSkip/size*size)
			got := hold("skip", "records.find", page(from, step, []string{"_id"}))
			if prob, ok := got.(*Problem); ok {
				problems = append(problems, *prob)
				break
			}
			m, _ := got.(map[string]any)
			if m == nil {
				loading = true
				break
			}
			next, _ := m["next"].(string)
			if next == "" {
				break // there are fewer records than that
			}
			cursors[strconv.Itoa(known+step/size)] = next
			k-- // and try page k again
			continue
		}
		got := hold("page:"+strconv.Itoa(k), "records.find", page(cur, size, nil))
		if prob, ok := got.(*Problem); ok {
			problems = append(problems, *prob)
			break
		}
		m, _ := got.(map[string]any)
		if m == nil {
			loading = true
			break
		}
		if len(rows) == 0 {
			offset = k * size
		}
		list, _ := m["records"].([]any)
		for _, rec := range list {
			id := fmt.Sprint(getPath(rec, "_id"))
			if seen[id] {
				continue // a record that moved between pages
			}
			seen[id] = true
			rows = append(rows, rec)
		}
		next, _ := m["next"].(string)
		more = next != ""
		if !more {
			break
		}
		cursors[strconv.Itoa(k+1)] = next
	}
	// The total, for the scrollbar
	countParams := map[string]any{"entity": entity, "aggregates": []any{map[string]any{"op": "count"}}}
	for _, p := range []string{"where", "filter"} {
		if v, ok := params[p]; ok {
			countParams[p] = v
		}
	}
	count := offset + len(rows)
	if more {
		count += size
	}
	if got := hold("count", "records.aggregate", countParams); got != nil {
		if n, ok := getPath(got, "groups.0.values.count").(float64); ok && int(n) >= offset+len(rows) {
			count = int(n)
		}
	}
	// Queries the window no longer needs end
	for name, q := range s.queries {
		if strings.HasPrefix(name, here+"#") && !used[name] && q.owner == sc.inst {
			q.cancel()
			delete(s.queries, name)
		}
	}
	if len(problems) > 0 {
		return ErrorNode(key, problems)
	}
	if out.Props == nil {
		out.Props = map[string]any{}
	}
	for _, p := range []string{"where", "filter", "fields", "expand"} {
		delete(out.Props, p) // the server's; sort stays for a grid's headers
		delete(out.Bind, p)
	}
	ids := make([]any, len(rows))
	for i, rec := range rows {
		ids[i] = getPath(rec, "_id")
	}
	out.P("count", count, "offset", offset, "ids", ids, "loading", loading, "pageSize", size)
	if out.On == nil {
		out.On = map[string]On{}
	}
	out.On["window"] = On{Handler: windowHandler + statePath, Payload: map[string]any{"first": "event.first", "last": "event.last"}}
	if n.Type == VirtualGridType {
		out.Props["rows"] = rows
		return out
	}
	// A virtualList's rows: its template once per record
	if len(n.Children) == 0 {
		return out
	}
	as, _ := n.Props["as"].(string)
	if as == "" {
		as = "row"
	}
	here2 := append(slices.Clone(path), key)
	for i, rec := range rows {
		rs := &scope{inst: sc.inst, rows: maps.Clone(sc.rows), slots: sc.slots, outer: sc.outer}
		if rs.rows == nil {
			rs.rows = map[string]any{}
		}
		rs.rows[as] = rec
		k := key + "_" + unsafeKey.ReplaceAllString(fmt.Sprint(ids[i]), "_")
		out.Children = append(out.Children, e.nodes(r, rs, n.Children[0], here2, k)...)
	}
	return out
}

// boundValue is a prop of an evaluated primitive: its value, or what its
// state.* or query: binding reads, as a dependency of the render.
func (e *evaluator) boundValue(r *Render, out *Node, prop string) any {
	if src, ok := out.Bind[prop]; ok {
		switch {
		case strings.HasPrefix(src, "state."):
			return normalize(r.State(src[6:]))
		case strings.HasPrefix(src, "query:"):
			name, path, _ := strings.Cut(src[6:], ".")
			if path == "" {
				return normalize(r.Query(name))
			}
			return normalize(r.Query(name, path))
		}
		return nil
	}
	return out.Props[prop]
}

func intProp(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return def
}

// windowEvent keeps the rows a windowed collection shows, {first, last},
// at its state path; the region holding it renders a new window when that
// moves it.
func (s *Session) windowEvent(statePath string, payload map[string]any) {
	if !strings.HasPrefix(statePath, "instances.") || !strings.Contains(statePath, "."+windowState) {
		return
	}
	st, _ := getPath(s.state, statePath).(map[string]any)
	if st == nil {
		return
	}
	first, last := intProp(payload["first"], -1), intProp(payload["last"], -1)
	if first < 0 || last < first || last-first > 10*maxPage {
		return
	}
	if intProp(st["first"], -1) == first && intProp(st["last"], -1) == last {
		return
	}
	st["first"], st["last"] = first, last
	s.changed = append(s.changed, "state."+statePath+".first", "state."+statePath+".last")
	s.flush(nil)
}
