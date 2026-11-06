package recordsnodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

type fixture struct {
	t    *testing.T
	p    *project.Projections
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	eng  *engine.Engine
	deps data.Deps
	n    atomic.Int64
}

// node is a graph node: id, type and config.
func node(id, typ string, config any) cognition.Node {
	raw, _ := json.Marshal(config)
	if config == nil {
		raw = nil
	}
	return cognition.Node{ID: id, Type: typ, Config: raw}
}

// edges joins "node.pin -> node.pin" pairs.
func edges(pairs ...string) []cognition.Edge {
	var out []cognition.Edge
	for _, p := range pairs {
		a, b, _ := strings.Cut(p, " -> ")
		fn, fp, _ := strings.Cut(a, ".")
		tn, tp, _ := strings.Cut(b, ".")
		out = append(out, cognition.Edge{From: cognition.PinRef{Node: fn, Pin: fp}, To: cognition.PinRef{Node: tn, Pin: tp}})
	}
	return out
}

func spec(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// The graphs under test, built in so the bus and the subscriptions find them.
var graphs = []cognition.Graph{
	{
		// Every write node in one command: two new tasks, and a change, an
		// add, a remove and a delete on existing ones
		ID: "cmd-task-plan", Name: "task.plan", Kind: livenodes.KindCommand,
		Nodes: []cognition.Node{
			node("event", livenodes.OnCommand, map[string]any{"outputs": []any{
				map[string]any{"id": "title", "type": "string"}, map[string]any{"id": "existing", "type": "string"}, map[string]any{"id": "gone", "type": "string"}}}),
			node("v1", "data.expression", map[string]any{"expr": "{title: title}", "type": "json"}),
			node("v2", "data.expression", map[string]any{"expr": "{title: title + \" (2)\", tags: [\"new\"]}", "type": "json"}),
			node("set", "data.expression", map[string]any{"expr": "{done: true}", "type": "json"}),
			node("add", "data.expression", map[string]any{"expr": "[\"c\"]", "type": "json"}),
			node("rm", "data.expression", map[string]any{"expr": "[\"a\"]", "type": "json"}),
			node("c1", "records.create", map[string]any{"entity": "task"}),
			node("c2", "records.create", map[string]any{"entity": "task"}),
			node("up", "records.update", map[string]any{"entity": "task"}),
			node("addTo", "records.addTo", map[string]any{"entity": "task", "field": "tags"}),
			node("removeFrom", "records.removeFrom", map[string]any{"entity": "task", "field": "tags"}),
			node("del", "records.delete", map[string]any{"entity": "task"}),
		},
		Edges: edges("event.then -> c1.then", "c1.then -> c2.then", "c2.then -> up.then", "up.then -> addTo.then", "addTo.then -> removeFrom.then", "removeFrom.then -> del.then",
			"event.title -> v1.title", "event.title -> v2.title", "v1.value -> c1.values", "v2.value -> c2.values",
			"event.existing -> up.id", "set.value -> up.set", "event.existing -> addTo.id", "add.value -> addTo.items",
			"event.existing -> removeFrom.id", "rm.value -> removeFrom.items", "event.gone -> del.id"),
		Spec: spec(livenodes.CommandSpec{Command: "task.plan", Title: "Plan", Description: "Writes several tasks at once.",
			Area: "rec", TargetType: "plan", Projection: data.Projector, Action: "record.create", Events: []string{"created", "updated", "added", "removed", "deleted"}}),
	},
	{
		ID: "qry-task-open", Name: "task.open", Kind: livenodes.KindQuery,
		Nodes: []cognition.Node{
			node("event", livenodes.OnQuery, nil),
			node("count", "records.aggregate", map[string]any{"entity": "task", "where": map[string]any{"done": false}}),
			node("ret", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "result", "type": "json"}}}),
		},
		Edges: edges("event.then -> ret.then", "count.values -> ret.result"),
		Spec:  spec(livenodes.QuerySpec{Query: "task.open", Title: "Open tasks", Description: "How many tasks are open.", Action: "record.read"}),
	},
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	var liveDeps livenodes.Deps
	var draftDeps drafts.Deps
	var entityDeps entities.Deps
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	var deps data.Deps
	livenodes.Contribute(reg, func() livenodes.Deps { return liveDeps })
	uinodes.Contribute(reg)
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return deps })
	recordsnodes.Contribute(reg, func() data.Deps { return deps })
	for i, g := range graphs {
		cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: g.ID, Order: i}, Graph: g})
	}
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, cognition.Models()...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	f := &fixture{t: t, p: p, bus: bus.New(p), subs: subscribe.New(p, reg), eng: engine.New(gdb, reg, nil)}
	deps = data.Deps{Projections: p, Bus: f.bus}
	f.deps = deps
	entityDeps = entities.Deps{Projections: p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, deps) }}
	draftDeps = drafts.Deps{Engine: f.eng, Projections: p}
	liveDeps = livenodes.Deps{Engine: f.eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	f.bus.Resolve = livenodes.CommandFor(func() livenodes.Deps { return liveDeps })
	f.subs.Resolve = livenodes.QueryFor(func() livenodes.Deps { return liveDeps })
	for _, g := range graphs {
		if res := cognition.CatalogOf(reg).Resolve(g); len(res.Problems) > 0 {
			t.Fatalf("%s: %+v", g.ID, res.Problems)
		}
	}
	f.ok("entity.create", "task", map[string]any{"name": "Task", "fields": []any{
		map[string]any{"name": "title", "type": "text", "required": true},
		map[string]any{"name": "done", "type": "bool", "default": false},
		map[string]any{"name": "tags", "type": "tags"},
	}})
	res := f.ok("entity.publish", "task", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.task", res.Seq)
	return f
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) on(typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.on(typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) wait(projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", projector, key, seq, err)
	}
	return v
}

// tasks are every task by title, once the projection has folded the stream.
func (f *fixture) tasks() map[string]data.Record {
	f.t.Helper()
	if err := f.p.Settle(context.Background(), features.LocalTenantID, data.Projector); err != nil {
		f.t.Fatal(err)
	}
	res, err := data.Find(args(), f.deps, data.Query{Entity: "task"})
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]data.Record{}
	for _, r := range res.Records {
		var title string
		_ = json.Unmarshal(r["title"], &title)
		out[title] = r
	}
	return out
}

func TestWriteNodesCommitAsOneBatch(t *testing.T) {
	f := setup(t)
	f.ok("record.create", "e1", map[string]any{"entity": "task", "values": map[string]any{"title": "Existing", "tags": []string{"a", "b"}}})
	f.ok("record.create", "g1", map[string]any{"entity": "task", "values": map[string]any{"title": "Gone"}})

	res := f.ok("task.plan", "p1", map[string]any{"title": "Planned", "existing": "e1", "gone": "g1"})
	got := f.tasks()
	if len(got) != 3 || got["Planned"] == nil || got["Planned (2)"] == nil || got["Gone"] != nil {
		t.Fatalf("want Existing, Planned and Planned (2), got %v", got)
	}
	e := got["Existing"]
	if string(e["done"]) != "true" || string(e["tags"]) != `["b","c"]` {
		t.Errorf("Existing should be done with tags b and c: %s %s", e["done"], e["tags"])
	}
	if string(got["Planned (2)"]["tags"]) != `["new"]` || string(got["Planned"]["done"]) != "false" {
		t.Errorf("the new tasks lack their values or defaults: %v", got)
	}
	// Six events, committed together: the last one's batch counts them all
	stream, err := f.p.Stream(context.Background(), features.LocalTenantID, live.AreaRecords)
	if err != nil {
		t.Fatal(err)
	}
	m, err := stream.GetMsg(context.Background(), res.Seq)
	if err != nil {
		t.Fatal(err)
	}
	var ev live.Event
	_ = json.Unmarshal(m.Data, &ev)
	if ev.Batch == nil || ev.Batch.Count != 6 {
		t.Errorf("want one batch of 6 events, got %+v", ev.Batch)
	}

	// A write the record command would refuse refuses the whole command,
	// and nothing commits
	before := len(f.tasks())
	bad := f.on("task.plan", "p2", map[string]any{"title": "Never", "existing": "nope", "gone": "e1"})
	if bad.Common.Code != 422 || !strings.Contains(fmt.Sprint(bad.Problems), `no task record "nope"`) {
		t.Errorf("want the missing record refused, got %d %+v", bad.Common.Code, bad.Problems)
	}
	if after := f.tasks(); len(after) != before || after["Never"] != nil || after["Existing"] == nil {
		t.Errorf("a refused command committed something: %v", after)
	}
	// So does a value its field refuses
	bad = f.on("task.plan", "p3", map[string]any{"existing": "e1", "gone": "e1"})
	if bad.Common.Code != 422 || !strings.Contains(fmt.Sprint(bad.Problems), "required") {
		t.Errorf("want the missing title refused, got %d %+v", bad.Common.Code, bad.Problems)
	}
}

// host is a handler's host, recording the view state it sets.
type host struct{ state map[string]any }

func (h *host) State(path string) any      { return h.state[path] }
func (h *host) Set(path string, v any)     { h.state[path] = v }
func (h *host) Value(string) any           { return nil }
func (h *host) Do(ui.Action)               {}
func (h *host) OpenEditor(_, _, _, _ string, _ any) {}
func (h *host) Command(string, string, any) ([]live.Problem, *features.ServiceResponse) {
	return nil, nil
}

func TestReadNodesInAHandler(t *testing.T) {
	f := setup(t)
	f.ok("record.create", "r1", map[string]any{"entity": "task", "values": map[string]any{"title": "One", "done": true}})
	f.ok("record.create", "r2", map[string]any{"entity": "task", "values": map[string]any{"title": "Two"}})
	f.ok("record.create", "r3", map[string]any{"entity": "task", "values": map[string]any{"title": "Three"}})
	f.tasks()

	g := cognition.Graph{ID: "on-read", Kind: uinodes.KindHandler,
		Nodes: []cognition.Node{
			node("event", uinodes.OnEvent, nil),
			node("id", "data.expression", map[string]any{"expr": "\"r1\"", "type": "string"}),
			node("get", "records.get", map[string]any{"entity": "task"}),
			node("missing", "records.get", map[string]any{"entity": "task"}),
			node("nope", "data.expression", map[string]any{"expr": "\"nope\"", "type": "string"}),
			node("query", "records.query", map[string]any{"entity": "task", "where": map[string]any{"done": false}, "sort": []any{map[string]any{"field": "title", "desc": true}}}),
			node("count", "records.aggregate", map[string]any{"entity": "task", "groupBy": "done", "aggregates": []any{map[string]any{"op": "count"}}}),
			node("s1", "ui.setState", map[string]any{"path": "record"}),
			node("s2", "ui.setState", map[string]any{"path": "exists"}),
			node("s3", "ui.setState", map[string]any{"path": "open"}),
			node("s4", "ui.setState", map[string]any{"path": "groups"}),
		},
		Edges: edges("event.then -> s1.then", "s1.then -> s2.then", "s2.then -> s3.then", "s3.then -> s4.then",
			"id.value -> get.id", "get.record -> s1.value", "nope.value -> missing.id", "missing.exists -> s2.value",
			"query.records -> s3.value", "count.groups -> s4.value"),
	}
	h := &host{state: map[string]any{}}
	if _, err := f.eng.Run(engine.RunArgs{ServiceArgs: args(), Graph: &g, Event: uinodes.OnEvent, Host: h}); err != nil {
		t.Fatal(err)
	}
	rec, _ := h.state["record"].(map[string]any)
	if rec["title"] != "One" || rec["_id"] != "r1" {
		t.Errorf("Get record: %v", h.state["record"])
	}
	if h.state["exists"] != false {
		t.Errorf("a missing record exists: %v", h.state["exists"])
	}
	open, _ := json.Marshal(h.state["open"])
	if !strings.Contains(string(open), `"title":"Two"`) || strings.Index(string(open), "Two") > strings.Index(string(open), "Three") || strings.Contains(string(open), "One") {
		t.Errorf("Query: want Two then Three, got %s", open)
	}
	groups, _ := json.Marshal(h.state["groups"])
	if string(groups) != `[{"key":false,"values":{"count":2}},{"key":true,"values":{"count":1}}]` {
		t.Errorf("Aggregate: %s", groups)
	}

	// Write nodes run only in a command graph
	w := cognition.Graph{ID: "on-write", Kind: uinodes.KindHandler,
		Nodes: []cognition.Node{node("event", uinodes.OnEvent, nil), node("del", "records.delete", map[string]any{"entity": "task"})},
		Edges: edges("event.then -> del.then")}
	if _, err := f.eng.Run(engine.RunArgs{ServiceArgs: args(), Graph: &w, Event: uinodes.OnEvent, Host: h}); err == nil {
		t.Errorf("a write node ran in a handler")
	}
}

// sink collects a subscription's results.
type sink struct {
	data chan any
}

func (s *sink) Snapshot(d any, _ live.Position) { s.data <- d }
func (s *sink) Change(d live.Delta, _ live.Position) {
	var v any
	_ = json.Unmarshal(d.Value, &v)
	s.data <- v
}
func (s *sink) Failed(r features.ServiceResponse) { s.data <- r.Message }

func (s *sink) next(t *testing.T) string {
	t.Helper()
	select {
	case d := <-s.data:
		raw, _ := json.Marshal(d)
		return string(raw)
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
		return ""
	}
}

func TestAQueryGraphFollowsTheRecords(t *testing.T) {
	f := setup(t)
	f.ok("record.create", "r1", map[string]any{"entity": "task", "values": map[string]any{"title": "One"}})
	f.tasks()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := args()
	a.Ctx = ctx
	s := &sink{data: make(chan any, 16)}
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: a, Query: "task.open", Params: json.RawMessage(`{}`), Sink: s}); fail != nil {
		t.Fatal(fail.Message)
	}
	if got := s.next(t); got != `{"count":1}` {
		t.Fatalf("snapshot: %s", got)
	}
	f.ok("record.create", "r2", map[string]any{"entity": "task", "values": map[string]any{"title": "Two"}})
	if got := s.next(t); got != `{"count":2}` {
		t.Fatalf("after a create: %s", got)
	}
	f.ok("record.update", "r1", map[string]any{"entity": "task", "set": map[string]any{"done": true}})
	if got := s.next(t); got != `{"count":1}` {
		t.Fatalf("after a toggle: %s", got)
	}
	// Several changes at once run it again once they settle
	for i := range 5 {
		f.ok("record.create", fmt.Sprintf("n%d", i), map[string]any{"entity": "task", "values": map[string]any{"title": "New"}})
	}
	deadline := time.After(5 * time.Second)
	for {
		if got := s.next(t); got == `{"count":6}` {
			break
		}
		select {
		case <-deadline:
			t.Fatal("never reached 6")
		default:
		}
	}
}
