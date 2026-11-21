package data

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// item is one row of a result's list: its key and value as sent.
type item struct {
	key   string
	value json.RawMessage
}

// computed is a result worked out afresh: the whole of it for a snapshot,
// its list's rows, and the keys beside the list that change with it (a
// page's next), empty for none.
type computed struct {
	result any
	items  []item
	keys   map[string]json.RawMessage
}

// rerun feeds a query that reads more than one record per row, so a change
// cannot be applied to what it holds: it runs again, debounced, after
// changes to the entity types it reads, and the subscriber gets the
// differences (Records.md › Built in slice 20).
func rerun(f live.Feed, d Deps, entityIDs []string, list string, compute func() (computed, error)) error {
	tenant := f.Args().Principal.TenantID
	// Changes are followed from before the first run
	updates := make(chan live.Position, 64)
	for _, id := range entityIDs {
		ch, err := d.Projections.Updates(f.Context(), tenant, Projector, id+".*")
		if err != nil {
			return err
		}
		go func() {
			for e := range ch {
				select {
				case updates <- e.Value.Position():
				case <-f.Context().Done():
					return
				}
			}
		}()
	}
	return rerunOn(f, d, updates, list, compute)
}

// rerunOn is rerun after each change updates sends, with the position it
// reached: records.search runs again as the search index changes.
func rerunOn(f live.Feed, d Deps, updates <-chan live.Position, list string, compute func() (computed, error)) error {
	args := f.Args()
	tenant := args.Principal.TenantID
	run := func(pos live.Position) (computed, live.Position, error) {
		folded, err := d.Projections.Folded(f.Context(), tenant, Projector)
		if err != nil {
			return computed{}, pos, err
		}
		c, err := compute()
		return c, pos.Merge(folded), err
	}
	cur, pos, err := run(live.Position{})
	if err != nil {
		return err
	}
	f.Snapshot(cur.result, pos)
	wait := time.Duration(d.settings(args).DebounceMs) * time.Millisecond
	if wait <= 0 {
		wait = 100 * time.Millisecond
	}
	for {
		select {
		case <-f.Context().Done():
			return nil
		case at := <-updates:
			pos = pos.Merge(at)
		}
		// Every change within the window is one run
		timer := time.NewTimer(wait)
	window:
		for {
			select {
			case <-f.Context().Done():
				timer.Stop()
				return nil
			case at := <-updates:
				pos = pos.Merge(at)
			case <-timer.C:
				break window
			}
		}
		next, at, err := run(pos)
		if err != nil {
			return err
		}
		pos = at
		sent := false
		for _, delta := range diff(cur.items, next.items) {
			delta.List = list
			f.Change(delta, pos)
			sent = true
		}
		for k, v := range next.keys {
			if string(cur.keys[k]) != string(v) {
				f.Change(live.Delta{Op: "upsert", Key: k, Value: v}, pos)
				sent = true
			}
		}
		for k := range cur.keys {
			if _, ok := next.keys[k]; !ok {
				f.Change(live.Delta{Op: "remove", Key: k}, pos)
				sent = true
			}
		}
		if !sent {
			f.Change(live.Reached, pos)
		}
		cur = next
	}
}

// diff is the list changes that turn old into new: rows that left it
// removed, then each row of new in turn moved, updated or inserted where
// it belongs. Indexes are the list's as the changes before left it.
func diff(old, new []item) []live.Delta {
	in := make(map[string]bool, len(new))
	for _, it := range new {
		in[it.key] = true
	}
	var out []live.Delta
	cur := slices.Clone(old)
	for i := len(cur) - 1; i >= 0; i-- {
		if !in[cur[i].key] {
			out = append(out, live.Delta{Op: "remove", Key: cur[i].key, From: ptr(i)})
			cur = slices.Delete(cur, i, i+1)
		}
	}
	for i, it := range new {
		j := slices.IndexFunc(cur, func(c item) bool { return c.key == it.key })
		switch {
		case j == i:
			if string(cur[i].value) != string(it.value) {
				out = append(out, live.Delta{Op: "update", Key: it.key, From: ptr(i), Value: it.value})
			}
		case j > i:
			out = append(out, live.Delta{Op: "move", Key: it.key, From: ptr(j), Index: ptr(i), Value: it.value})
			cur = slices.Insert(slices.Delete(cur, j, j+1), i, it)
			continue
		default:
			out = append(out, live.Delta{Op: "insert", Key: it.key, Index: ptr(i), Value: it.value})
			cur = slices.Insert(cur, i, it)
			continue
		}
		cur[i] = it
	}
	return out
}

// rerunFind is records.find for a query with expansions: its page, run
// again on every change to the records it shows or expands.
func rerunFind(f live.Feed, d Deps, p *planned) error {
	return rerun(f, d, p.targets(), ListName, func() (computed, error) {
		args := f.Args()
		res, rows, err := p.read(args, d)
		if err != nil {
			return computed{}, err
		}
		if !res.Plan.Complete && !res.Plan.Ordered {
			return computed{}, log.Code("scan_limit").With("entity", p.e.ID).Errorf("%s has more than %d records, more than a live query reads without an index", p.e.ID, d.settings(args).ScanLimit)
		}
		res = p.page(res, rows)
		if err := p.expandAll(args, d, res.Records, rows); err != nil {
			return computed{}, err
		}
		c := computed{result: res, keys: map[string]json.RawMessage{}}
		for _, r := range res.Records {
			var id string
			_ = json.Unmarshal(r[MetaID], &id)
			c.items = append(c.items, item{key: id, value: raw(r)})
		}
		if res.Next != "" {
			c.keys["next"] = raw(res.Next)
		}
		return c, nil
	})
}

// runAggregate is records.aggregate's Run.
func runAggregate(deps func() Deps) func(live.Feed) error {
	return func(f live.Feed) error {
		d := deps()
		var q Aggregation
		_ = json.Unmarshal(f.Params(), &q)
		a, err := prepareAggregation(f.Args(), d, q)
		if err != nil {
			return err
		}
		return rerun(f, d, []string{a.p.e.ID}, GroupsName, func() (computed, error) {
			s, err := a.run(f.Args(), d)
			if err != nil {
				return computed{}, err
			}
			c := computed{result: s}
			for _, g := range s.Groups {
				c.items = append(c.items, item{key: GroupKey(g.Key), value: raw(g)})
			}
			return c, nil
		})
	}
}

// feed is a subscription's feed on a context of its own, which ends when
// the query must run afresh.
type feed struct {
	live.Feed
	ctx context.Context
}

func (f feed) Context() context.Context { return f.ctx }

func (f feed) Args() features.ServiceArgs {
	a := f.Feed.Args()
	a.Ctx = f.ctx
	return a
}

// republished runs a record query afresh, from a new snapshot, whenever an
// entity type it reads is published again or deleted, so its row rules,
// fields and indexes are always the published ones (Records.md › Built in
// slice 21). read names the types a query's parameters read.
func republished(deps func() Deps, read func(args features.ServiceArgs, d Deps, params json.RawMessage) []string, run func(live.Feed) error) func(live.Feed) error {
	return func(f live.Feed) error {
		d := deps()
		args := f.Args()
		for {
			ctx, cancel := context.WithCancel(f.Context())
			changes, err := d.Projections.Updates(ctx, args.Principal.TenantID, entities.PublishedProjector, entities.TargetType+".*")
			if err != nil {
				cancel()
				return err
			}
			versions := map[string]string{}
			for _, id := range read(args, d, f.Params()) {
				versions[id] = publishedVersion(args, d, id)
			}
			done := make(chan error, 1)
			go func() { done <- run(feed{Feed: f, ctx: ctx}) }()
			again := false
			for !again {
				select {
				case err := <-done:
					cancel()
					return err
				case e, ok := <-changes:
					if !ok {
						changes = nil
						continue
					}
					id := strings.TrimPrefix(e.Key, entities.TargetType+".")
					if v, read := versions[id]; read && v != versionOf(e.Value.Value) {
						again = true
					}
				}
			}
			cancel()
			<-done
			if f.Context().Err() != nil {
				return nil
			}
		}
	}
}

// publishedVersion is what tells one published version of an entity type
// from the next: its version and whether it is deleted.
func publishedVersion(args features.ServiceArgs, d Deps, id string) string {
	v, ok, err := d.Projections.Get(args.Context(), args.Principal.TenantID, entities.PublishedProjector, entities.TargetType+"."+id)
	if err != nil || !ok {
		return ""
	}
	return versionOf(v.Value)
}

func versionOf(value json.RawMessage) string {
	var e records.EntityType
	if json.Unmarshal(value, &e) != nil {
		return ""
	}
	return string(raw([]any{e.Version, e.Hidden}))
}

// queryReads names the entity types a records.find, records.list or
// records.get reads: its own, and those its expansions point to.
func queryReads(args features.ServiceArgs, d Deps, params json.RawMessage) []string {
	var q Query
	_ = json.Unmarshal(params, &q)
	out := []string{q.Entity}
	if len(q.Expand) == 0 {
		return out
	}
	e, problems := Entity(args, d.Projections, q.Entity)
	if problems != nil {
		return out
	}
	for _, id := range q.Expand {
		if f, _, ok := e.FieldByID(id); ok && f.Relation != nil && !slices.Contains(out, f.Relation.Entity) {
			out = append(out, f.Relation.Entity)
		}
	}
	return out
}
