package data

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// ListName is the list a records.find result's changes apply to.
const ListName = "records"

// maxBeyond bounds the matches past a window a live query remembers.
const maxBeyond = 256

// view is a records.find subscription: a query's page as its subscriber
// has it, kept current from the record projection's changes without
// running the query again (Records.md › Built in slice 19).
type view struct {
	p    *planned
	args features.ServiceArgs
	d    Deps
	f    live.Feed
	// rows are the matches after the cursor, in order. When all is set they
	// are every match, and the window is the first limit of them (all of
	// them without a limit). Otherwise the query is an ordered walk with a
	// limit and rows are the window alone. Then beyond holds the matches
	// that follow it, in order, up to its last: every one when unseen is
	// false, and when it is true more may follow, which a read of the index
	// finds once beyond runs out.
	rows   []row
	byID   map[string]row
	all    bool
	beyond []row
	unseen bool
	limit  int
	// What the subscriber has: the window's ids, their values and next
	shown  []string
	values map[string]json.RawMessage
	next   string
	pos    live.Position
	sent   bool // a change was sent for the last update
}

// runFind is records.find's Run: Find's page, then its changes.
func runFind(deps func() Deps) func(live.Feed) error {
	return func(f live.Feed) error {
		d := deps()
		args := f.Args()
		if err := Follow(args, d); err != nil {
			log.Error(args.Context(), err, "the converter cannot start")
		}
		var q Query
		_ = json.Unmarshal(f.Params(), &q)
		p, err := prepare(args, d, q)
		if err != nil {
			return err
		}
		// Changes are followed from before the page is read, so none is
		// missed; one the page already has is skipped by its _seq
		updates, err := d.Projections.Updates(f.Context(), args.Principal.TenantID, Projector, q.Entity+".*")
		if err != nil {
			return err
		}
		// The page reflects every event the projector had folded before it
		// was read; later ones arrive as updates
		pos, err := d.Projections.Folded(f.Context(), args.Principal.TenantID, Projector)
		if err != nil {
			return err
		}
		res, rows, err := p.read(args, d)
		if err != nil {
			return err
		}
		v := &view{p: p, args: args, d: d, f: f, limit: max(q.Limit, 0), byID: map[string]row{}, values: map[string]json.RawMessage{}, pos: pos}
		v.all = !res.Plan.Ordered || v.limit == 0
		if v.all && !res.Plan.Complete {
			return log.Code("scan_limit").With("entity", q.Entity).Errorf("%s has more than %d records, more than a live query reads without an index that gives its order", q.Entity, d.settings(args).ScanLimit)
		}
		if !v.all {
			v.unseen = len(rows) > v.limit || !res.Plan.Complete
			v.beyond = slices.Clone(rows[min(len(rows), v.limit):])
			rows = rows[:min(len(rows), v.limit)]
		}
		for _, r := range rows {
			v.rows = append(v.rows, r)
			v.byID[r.id] = r
		}
		page := p.page(res, v.window())
		for _, r := range v.window() {
			v.shown = append(v.shown, r.id)
			v.values[r.id] = v.value(r)
		}
		page.Next = v.cursor()
		v.next = page.Next
		f.Snapshot(page, v.pos)
		for e := range updates {
			if err := v.apply(e); err != nil {
				return err
			}
			// Once caught up, say how far the subscription has reached
			if !v.sent && len(updates) == 0 {
				f.Change(live.Reached, v.pos)
				v.sent = true
			}
		}
		return nil
	}
}

// window is what the subscriber is shown.
func (v *view) window() []row {
	if v.all && v.limit > 0 && len(v.rows) > v.limit {
		return v.rows[:v.limit]
	}
	return v.rows
}

// cursor is the next page's cursor: the window's last row, while matches
// past it exist.
func (v *view) cursor() string {
	w := v.window()
	more := len(v.rows) > v.limit || !v.all && (len(v.beyond) > 0 || v.unseen)
	if v.limit == 0 || len(w) == 0 || !more {
		return ""
	}
	last := w[len(w)-1]
	return cursor{Keys: last.keys, ID: last.id}.String()
}

func (v *view) value(r row) json.RawMessage {
	raw, _ := json.Marshal(v.p.fields(r.record))
	return raw
}

func (v *view) indexOf(id string) int {
	old, ok := v.byID[id]
	if !ok {
		return -1
	}
	if i, found := slices.BinarySearchFunc(v.rows, old, v.p.sort.compare); found {
		return i
	}
	return -1
}

func (v *view) insert(r row) {
	i, _ := slices.BinarySearchFunc(v.rows, r, v.p.sort.compare)
	v.rows = slices.Insert(v.rows, i, r)
	v.byID[r.id] = r
}

// toBeyond keeps a match past the window, unless it follows matches not
// read yet.
func (v *view) toBeyond(r row) {
	i, _ := slices.BinarySearchFunc(v.beyond, r, v.p.sort.compare)
	if v.unseen && i == len(v.beyond) {
		return
	}
	v.beyond = slices.Insert(v.beyond, i, r)
	if len(v.beyond) > maxBeyond {
		v.beyond = v.beyond[:maxBeyond]
		v.unseen = true
	}
}

// apply folds one change of a record into the view and sends what it
// changes for the subscriber.
func (v *view) apply(e *live.Entry) error {
	v.pos = v.pos.Merge(e.Value.Position())
	v.sent = false
	id := strings.TrimPrefix(e.Key, v.p.e.ID+".")
	var r Record
	if !e.Deleted && json.Unmarshal(e.Value.Value, &r) != nil {
		return nil
	}
	if i := v.indexOf(id); i >= 0 {
		if !e.Deleted && r.Seq() <= v.rows[i].record.Seq() {
			return nil // the page read it already
		}
		v.rows = slices.Delete(v.rows, i, i+1)
		delete(v.byID, id)
	}
	v.beyond = slices.DeleteFunc(v.beyond, func(b row) bool { return b.id == id })
	if !e.Deleted && v.p.filter.match(r) {
		nr := v.p.sort.row(r)
		if v.p.after == nil || v.p.sort.compare(nr, v.p.at()) > 0 {
			j, _ := slices.BinarySearchFunc(v.rows, nr, v.p.sort.compare)
			if v.all || j < len(v.rows) || len(v.rows) < v.limit && len(v.beyond) == 0 && !v.unseen {
				v.insert(nr)
			} else {
				v.toBeyond(nr)
			}
		}
	}
	if !v.all {
		for len(v.rows) > v.limit {
			last := v.rows[len(v.rows)-1]
			v.rows = v.rows[:len(v.rows)-1]
			delete(v.byID, last.id)
			v.toBeyond(last)
		}
		if err := v.refill(); err != nil {
			return err
		}
	}
	v.emit(id)
	return nil
}

// refill moves the matches after the window into it until it is full,
// and reads more from the index when the view runs out of them, so it
// knows whether any follow.
func (v *view) refill() error {
	for read := false; ; read = true {
		for len(v.rows) < v.limit && len(v.beyond) > 0 {
			v.insert(v.beyond[0])
			v.beyond = v.beyond[1:]
		}
		// A read that found only matches ahead of the view is tried again
		// on the next change
		if len(v.beyond) > 0 || !v.unseen || read {
			return nil
		}
		if err := v.read(v.limit - len(v.rows) + 4); err != nil {
			return err
		}
	}
}

// read reads up to n matches after the window into beyond. One whose value
// is ahead of the changes the view has applied waits for its change, and
// what follows it stays unseen.
func (v *view) read(n int) error {
	q := *v.p
	q.q.Limit = n
	if k := len(v.rows); k > 0 {
		q.after = &cursor{Keys: v.rows[k-1].keys, ID: v.rows[k-1].id}
	}
	res, rows, err := q.read(v.args, v.d)
	if err != nil {
		return err
	}
	stream := ""
	for s := range v.pos {
		stream = s
	}
	v.unseen = len(rows) > n || !res.Plan.Complete
	for _, r := range rows {
		if r.record.Seq() > v.pos[stream] {
			v.unseen = true
			break
		}
		v.beyond = append(v.beyond, r)
	}
	return nil
}

// emit sends the changes that turn the subscriber's window into the
// view's: rows that left it, the changed record's move or update, rows
// that entered it, and the next cursor.
func (v *view) emit(changed string) {
	w := v.window()
	at := make(map[string]int, len(w))
	for i, r := range w {
		at[r.id] = i
	}
	send := func(d live.Delta) {
		d.List = ListName
		v.f.Change(d, v.pos)
		v.sent = true
	}
	cur := v.shown
	for i := len(cur) - 1; i >= 0; i-- {
		if _, in := at[cur[i]]; !in {
			send(live.Delta{Op: "remove", Key: cur[i], From: ptr(i)})
			delete(v.values, cur[i])
			cur = slices.Delete(cur, i, i+1)
		}
	}
	// Every other row keeps its order, so the changed one moves among them
	if from := slices.Index(cur, changed); from >= 0 {
		to := 0
		for _, id := range cur {
			if id != changed && at[id] < at[changed] {
				to++
			}
		}
		value := v.value(w[at[changed]])
		switch {
		case to != from:
			send(live.Delta{Op: "move", Key: changed, From: ptr(from), Index: ptr(to), Value: value})
			cur = slices.Insert(slices.Delete(cur, from, from+1), to, changed)
		case string(value) != string(v.values[changed]):
			send(live.Delta{Op: "update", Key: changed, From: ptr(from), Value: value})
		}
		v.values[changed] = value
	}
	for i, r := range w {
		if i < len(cur) && cur[i] == r.id {
			continue
		}
		value := v.value(r)
		send(live.Delta{Op: "insert", Key: r.id, Index: ptr(i), Value: value})
		v.values[r.id] = value
		cur = slices.Insert(cur, i, r.id)
	}
	v.shown = cur
	if next := v.cursor(); next != v.next {
		v.next = next
		if next == "" {
			v.f.Change(live.Delta{Op: "remove", Key: "next"}, v.pos)
		} else {
			raw, _ := json.Marshal(next)
			v.f.Change(live.Delta{Op: "upsert", Key: "next", Value: raw}, v.pos)
		}
		v.sent = true
	}
}

func ptr(i int) *int { return &i }
