package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: features.LocalTenantID}}
}

func (f *fixture) by(b *bus.Bus, user uint64, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return b.Execute(bus.Request{ServiceArgs: as(user), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

// docRules are doc's row rules: people see their own docs and shared
// ones, create only their own, change only what they created, and delete
// only what they own.
var docRules = records.Access{
	Read:   "record.owner == principal.user || record.shared == true",
	Create: "record.owner == principal.user",
	Update: "record._createdBy == principal.user",
	Delete: "record.owner == principal.user",
}

// docs defines doc, with its rules, and note, whose about points to a doc.
func (f *fixture) docs() {
	f.t.Helper()
	f.ok("entity.create", "doc", map[string]any{"name": "Doc", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "owner", "type": "relation", "relation": map[string]any{"entity": "user"}},
		map[string]any{"name": "shared", "type": "bool"},
		map[string]any{"name": "n", "type": "number"},
	}})
	f.ok("entity.addIndex", "doc", map[string]any{"fields": []string{"n"}})
	f.ok("entity.setAccess", "doc", map[string]any{"access": docRules})
	f.publish("doc", 3)
	f.ok("entity.create", "note", map[string]any{"name": "Note", "fields": []any{
		map[string]any{"name": "text", "type": "text"},
		map[string]any{"name": "about", "type": "relation", "relation": map[string]any{"entity": "doc"}},
	}})
	f.publish("note", 1)
}

// sees is what docRules' read rule lets user see.
func sees(user uint64, r map[string]any) bool {
	return r["owner"] == strconv.FormatUint(user, 10) || r["shared"] == true
}

// guarded is a records.find sink that checks every row it is sent.
type guarded struct {
	*findSink
	user uint64
	mu   sync.Mutex
	leak string
}

func (g *guarded) check(v any) {
	m, _ := v.(map[string]any)
	if m != nil && !sees(g.user, m) {
		g.mu.Lock()
		g.leak = fmt.Sprintf("user %d was sent %v", g.user, m)
		g.mu.Unlock()
	}
}

func (g *guarded) Snapshot(d any, pos live.Position) {
	raw, _ := json.Marshal(d)
	var page struct{ Records []any }
	_ = json.Unmarshal(raw, &page)
	for _, r := range page.Records {
		g.check(r)
	}
	g.findSink.Snapshot(d, pos)
}

func (g *guarded) Change(d live.Delta, pos live.Position) {
	if d.List != "" && d.Value != nil {
		var v any
		_ = json.Unmarshal(d.Value, &v)
		g.check(v)
	}
	g.findSink.Change(d, pos)
}

// TestNoSubscriberReceivesAHiddenRecord is the acceptance test: random
// commands from several principals at once, while each of them holds
// records.find pages and a records.list; no change any of them receives
// shows a record their read rule hides, and every page ends equal to a
// fresh read as its principal.
func TestNoSubscriberReceivesAHiddenRecord(t *testing.T) {
	f := setup(t)
	f.docs()
	users := []uint64{2, 3, 4}
	r := rand.New(rand.NewPCG(seed, 21))
	var mu sync.Mutex
	var ids []string
	for range 40 {
		u := users[r.IntN(3)]
		res := f.by(f.bus, u, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{
			"title": fmt.Sprint("d", r.IntN(100)), "owner": strconv.FormatUint(u, 10), "shared": r.IntN(4) == 0, "n": r.IntN(20)}})
		if res.Common.Code >= 300 {
			t.Fatalf("create: %s %+v", res.Common.Message, res.Problems)
		}
		ids = append(ids, res.Target)
	}
	f.settle()

	type sub struct {
		user uint64
		q    data.Query
		g    *guarded
	}
	var subs []sub
	lists := map[uint64]*keyedSink{}
	for i := range 18 {
		u := users[i%3]
		q := data.Query{Entity: "doc", Sort: [][]data.Sort{nil, {{Field: "n"}}, {{Field: "title", Desc: true}}}[i%3], Limit: []int{0, 3, 7}[(i/3)%3]}
		g := &guarded{findSink: &findSink{shownAt: map[string]live.Position{}}, user: u}
		raw, _ := json.Marshal(q)
		if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: as(u), Query: "records.find", Params: raw, Sink: g}); fail != nil {
			t.Fatal(fail.Message)
		}
		subs = append(subs, sub{u, q, g})
	}
	for _, u := range users {
		s := &keyedSink{}
		if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: as(u), Query: "records.list", Params: json.RawMessage(`{"entity":"doc"}`), Sink: s}); fail != nil {
			t.Fatal(fail.Message)
		}
		lists[u] = s
	}

	var wg sync.WaitGroup
	var refused, accepted int
	for c, u := range users {
		b := bus.New(f.p)
		cr := rand.New(rand.NewPCG(seed+uint64(c), 23))
		wg.Go(func() {
			for range 60 {
				mu.Lock()
				id := ids[cr.IntN(len(ids))]
				mu.Unlock()
				var res bus.Result
				switch k := cr.IntN(10); {
				case k < 2:
					owner := users[cr.IntN(3)] // others' are refused
					res = f.by(b, u, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"title": "new", "owner": strconv.FormatUint(owner, 10), "n": cr.IntN(20)}})
					if res.Common.Code < 300 {
						mu.Lock()
						ids = append(ids, res.Target)
						mu.Unlock()
					}
				case k < 8:
					set := map[string]any{"n": cr.IntN(20)}
					if cr.IntN(2) == 0 {
						set["owner"] = strconv.FormatUint(users[cr.IntN(3)], 10)
					}
					if cr.IntN(3) == 0 {
						set["shared"] = cr.IntN(2) == 0
					}
					res = f.by(b, u, "record.update", id, map[string]any{"entity": "doc", "set": set})
				default:
					res = f.by(b, u, "record.delete", id, map[string]any{"entity": "doc"})
				}
				mu.Lock()
				if res.Common.Code >= 300 {
					refused++
				} else {
					accepted++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	f.settle()
	t.Logf("%d commands accepted, %d refused by their rules", accepted, refused)
	if refused == 0 || accepted == 0 {
		t.Fatalf("the rules refused %d and allowed %d", refused, accepted)
	}
	for _, s := range subs {
		f.matchesAs(t, as(s.user), s.g.findSink, s.q)
		if s.g.leak != "" {
			t.Fatal(s.g.leak)
		}
	}
	for _, u := range users {
		eventually(t, func() string {
			res, _ := data.Find(as(u), *f.deps, data.Query{Entity: "doc"})
			lists[u].mu.Lock()
			defer lists[u].mu.Unlock()
			for _, v := range lists[u].sent {
				var m map[string]any
				_ = json.Unmarshal(v, &m)
				if !sees(u, m) {
					t.Fatalf("records.list sent user %d %v", u, m)
				}
			}
			if len(lists[u].data) != len(res.Records) {
				return fmt.Sprintf("user %d's list has %d records, a fresh read %d", u, len(lists[u].data), len(res.Records))
			}
			return ""
		})
	}
	// An aggregate counts only what its principal sees
	for _, u := range users {
		all, _ := data.Summarize(args(), *f.deps, data.Aggregation{Entity: "doc", Aggregates: []data.Aggregate{{Op: "count"}}})
		mine, _ := data.Summarize(as(u), *f.deps, data.Aggregation{Entity: "doc", Aggregates: []data.Aggregate{{Op: "count"}}})
		page, _ := data.Find(as(u), *f.deps, data.Query{Entity: "doc"})
		if canon(mine.Groups[0].Values["count"]) != fmt.Sprint(len(page.Records)) || canon(all.Groups[0].Values["count"]) == canon(mine.Groups[0].Values["count"]) {
			t.Fatalf("user %d counts %s of %s, and sees %d", u, mine.Groups[0].Values["count"], all.Groups[0].Values["count"], len(page.Records))
		}
	}
}

// keyedSink keeps a records.list result, every value it was sent.
type keyedSink struct {
	mu   sync.Mutex
	data map[string]json.RawMessage
	sent []json.RawMessage
}

func (s *keyedSink) Snapshot(d any, _ live.Position) {
	raw, _ := json.Marshal(d)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &s.data)
	for _, v := range s.data {
		s.sent = append(s.sent, v)
	}
}

func (s *keyedSink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch d.Op {
	case "upsert":
		s.data[d.Key] = d.Value
		s.sent = append(s.sent, d.Value)
	case "remove":
		delete(s.data, d.Key)
	}
}

func (s *keyedSink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

// TestRowRulesInCommandsAndQueries: each rule on its own.
func TestRowRulesInCommandsAndQueries(t *testing.T) {
	f := setup(t)
	f.docs()
	mine := f.by(f.bus, 2, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"title": "mine", "owner": "2"}})
	theirs := f.by(f.bus, 3, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"title": "theirs", "owner": "3"}})
	if mine.Common.Code >= 300 || theirs.Common.Code >= 300 {
		t.Fatal(mine.Common.Message, theirs.Common.Message)
	}
	refusedBy := func(res bus.Result, want string) {
		t.Helper()
		if res.Common.Code != 422 || !strings.Contains(fmt.Sprint(res.Problems), want) {
			t.Errorf("not refused with %q: %d %+v", want, res.Common.Code, res.Problems)
		}
	}
	// Creating another's doc is refused by the create rule
	refusedBy(f.by(f.bus, 2, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"title": "x", "owner": "3"}}), "create rule")
	// A doc the read rule hides is not there for its commands
	refusedBy(f.by(f.bus, 2, "record.update", theirs.Target, map[string]any{"entity": "doc", "set": map[string]any{"title": "mine now"}}), "there is no doc record")
	refusedBy(f.by(f.bus, 2, "record.delete", theirs.Target, map[string]any{"entity": "doc"}), "there is no doc record")
	// A shared doc can be seen, but not changed by who did not create it
	f.by(f.bus, 3, "record.update", theirs.Target, map[string]any{"entity": "doc", "set": map[string]any{"shared": true}})
	f.settle()
	refusedBy(f.by(f.bus, 2, "record.update", theirs.Target, map[string]any{"entity": "doc", "set": map[string]any{"title": "mine now"}}), "update rule")
	refusedBy(f.by(f.bus, 2, "record.delete", theirs.Target, map[string]any{"entity": "doc"}), "delete rule")
	if res := f.by(f.bus, 2, "record.update", theirs.Target, map[string]any{"entity": "doc", "set": map[string]any{"title": "x"}}); res.Problems[0].Code != "row_rule" {
		t.Errorf("a rule's refusal has code %q", res.Problems[0].Code)
	}
	// Service principals skip the rules
	if res := f.bus.Execute(bus.Request{ServiceArgs: features.BackgroundArgs(context.Background()), ID: "service-1", Type: "record.update", Target: theirs.Target, Args: json.RawMessage(`{"entity":"doc","set":{"shared":false}}`)}); res.Common.Code >= 300 {
		t.Fatalf("a service principal was refused: %+v", res.Problems)
	}

	// A doc reassigned away is removed from its old owner's open queries
	q := data.Query{Entity: "doc", Sort: []data.Sort{{Field: "title"}}}
	page := f.findAs(as(2), q)
	get := &aggSink{}
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: as(2), Query: "records.get", Params: json.RawMessage(`{"entity":"doc","id":"` + mine.Target + `"}`), Sink: get}); fail != nil {
		t.Fatal(fail.Message)
	}
	f.matchesAs(t, as(2), page, q)
	if ids, _, _ := page.view(); len(ids) != 1 || ids[0] != mine.Target {
		t.Fatalf("user 2 sees %v", ids)
	}
	if res := f.by(f.bus, 2, "record.update", mine.Target, map[string]any{"entity": "doc", "set": map[string]any{"owner": "3"}}); res.Common.Code >= 300 {
		t.Fatalf("reassigning: %+v", res.Problems)
	}
	eventually(t, func() string {
		ids, _, bad := page.view()
		get.mu.Lock()
		defer get.mu.Unlock()
		if bad != "" || len(ids) != 0 || get.data != nil {
			return fmt.Sprintf("after reassigning, user 2 still sees %v and %v %s", ids, get.data, bad)
		}
		return ""
	})

	// An expansion leaves out the docs its principal cannot see
	note := f.by(f.bus, 2, "record.create", "", map[string]any{"entity": "note", "values": map[string]any{"text": "about it", "about": mine.Target}})
	f.settle()
	for user, want := range map[uint64]bool{2: false, 3: true} {
		res, err := data.Find(as(user), *f.deps, data.Query{Entity: "note", Expand: []string{"about"}})
		if err != nil || len(res.Records) != 1 {
			t.Fatalf("%v %v", err, res.Records)
		}
		var e map[string]any
		_ = json.Unmarshal(res.Records[0][data.MetaExpanded], &e)
		if (e["about"] != nil) != want {
			t.Errorf("user %d expands about to %v", user, e["about"])
		}
	}
	_ = note

	// Publishing new rules runs open queries afresh: with no read rule,
	// user 2 sees both docs again
	all := f.findAs(as(2), q)
	f.ok("entity.setAccess", "doc", map[string]any{"access": records.Access{Create: docRules.Create}})
	f.publish("doc", 5)
	eventually(t, func() string {
		for _, s := range []*findSink{page, all} {
			if ids, _, bad := s.view(); len(ids) != 2 || bad != "" {
				return fmt.Sprintf("after the rule went, user 2 sees %v %s", ids, bad)
			}
		}
		return ""
	})

	// A rule that is not a bool expression is refused
	f.rejected("entity.setAccess", "doc", map[string]any{"access": map[string]any{"read": "record.owner =="}}, "access.read")
	f.rejected("entity.setAccess", "doc", map[string]any{"access": map[string]any{"update": "1 + 1"}}, "access.update")
	f.rejected("entity.setAccess", "doc", map[string]any{"access": map[string]any{"read": "user == 1"}}, "access.read")
}

// TestActionsNeedTheirGrant: without the record type's action, commands
// and queries are refused before any rule is read.
func TestActionsNeedTheirGrant(t *testing.T) {
	f := setup(t)
	f.docs()
	gdb, err := db.Open(db.WithPath(t.TempDir() + "/app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "tenant"}, Parent: "app",
		Actions: []authz.Action{{Name: "read", Class: authz.ClassRead}},
	})
	records.ContributeAccess(reg)
	pol := authz.NewPolicy(gdb, reg)
	ctx := context.Background()
	for user, role := range map[uint64]uint64{5: authz.RoleViewer, 6: authz.RoleEditor} {
		if _, err := pol.Grant(ctx, nil, authz.GrantArgs{UserID: user, RoleID: role, Resource: "tenant:1"}); err != nil {
			t.Fatal(err)
		}
	}
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })

	if res := f.by(f.bus, 5, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"owner": "5"}}); res.Common.Code != authz.Status {
		t.Errorf("a viewer created a record: %d %s", res.Common.Code, res.Common.Message)
	}
	res := f.by(f.bus, 6, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"owner": "6"}})
	if res.Common.Code >= 300 {
		t.Fatalf("an editor could not create their own: %s", res.Common.Message)
	}
	// Editors have no delete class
	if del := f.by(f.bus, 6, "record.delete", res.Target, map[string]any{"entity": "doc"}); del.Common.Code != authz.Status {
		t.Errorf("an editor deleted a record: %d %s", del.Common.Code, del.Common.Message)
	}
	// Reading needs record.read, which a principal with no grant lacks
	raw, _ := json.Marshal(data.Query{Entity: "doc"})
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: as(7), Query: "records.find", Params: raw, Sink: &findSink{shownAt: map[string]live.Position{}}}); fail == nil || fail.Code != authz.Status {
		t.Errorf("a principal without a grant opened records.find: %+v", fail)
	}
}
