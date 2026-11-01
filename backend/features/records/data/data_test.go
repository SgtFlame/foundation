package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	recordimpl "github.com/SgtFlame/foundation/backend/features/records/services/recordservice/impl"
	"github.com/nats-io/nats.go/jetstream"
)

type fixture struct {
	t    *testing.T
	reg  *extensions.Registry
	js   jetstream.JetStream
	p    *project.Projections
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	svc  recordservice.Service
	n    atomic.Int64
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	var entityDeps entities.Deps
	var deps data.Deps
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return deps })
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	f := &fixture{t: t, reg: reg, js: srv.JS(), p: p, bus: bus.New(p), subs: subscribe.New(p, reg)}
	deps = data.Deps{Projections: p, Bus: f.bus}
	entityDeps = entities.Deps{Projections: p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, deps) }}
	f.svc = recordimpl.New(recordimpl.WithRecords(f.bus, f.subs, deps))
	return f
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) on(b *bus.Bus, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return b.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.on(f.bus, typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) rejected(typ, target string, a any, want string) bus.Result {
	f.t.Helper()
	res := f.on(f.bus, typ, target, a)
	if res.Common.Code != 422 || !strings.Contains(fmt.Sprint(res.Problems), want) {
		f.t.Errorf("%s was not refused with %q: %d %+v", typ, want, res.Common.Code, res.Problems)
	}
	return res
}

// task defines and publishes Task, and waits until records can see it.
func (f *fixture) task() {
	f.ok("entity.create", "task", map[string]any{"name": "Task", "fields": []any{
		map[string]any{"name": "title", "type": "text", "required": true},
		map[string]any{"name": "done", "type": "bool", "default": false},
		map[string]any{"name": "due", "type": "date"},
		map[string]any{"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}},
		map[string]any{"name": "tags", "type": "tags"},
		map[string]any{"name": "points", "type": "text"},
	}})
	f.publish("task", 1)
}

func (f *fixture) publish(id string, rev int) {
	f.t.Helper()
	res := f.ok("entity.publish", id, map[string]any{"rev": rev})
	f.wait(entities.PublishedProjector, "entity."+id, res.Seq)
}

func (f *fixture) wait(projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", projector, key, seq, err)
	}
	return v
}

// record reads a record once its commit at seq is projected.
func (f *fixture) record(id string, seq uint64) data.Record {
	f.t.Helper()
	v := f.wait(data.Projector, data.Key("task", id), seq)
	var r data.Record
	_ = json.Unmarshal(v.Value, &r)
	return r
}

// events reads the record's events from the stream.
func (f *fixture) events(id string) []live.Event {
	f.t.Helper()
	s, err := f.js.Stream(context.Background(), live.StreamName(features.LocalTenantID, live.AreaRecords))
	if err != nil {
		f.t.Fatal(err)
	}
	var out []live.Event
	info, _ := s.Info(context.Background())
	for seq := uint64(1); seq <= info.State.LastSeq; seq++ {
		m, err := s.GetMsg(context.Background(), seq)
		if err != nil {
			continue
		}
		var e live.Event
		_ = json.Unmarshal(m.Data, &e)
		if e.Target.ID == id {
			out = append(out, e)
		}
	}
	return out
}

func str(v json.RawMessage) string { return string(v) }

func TestCommandsCommitTheirEvents(t *testing.T) {
	f := setup(t)
	f.task()

	res := f.ok("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Write the plan", "due": "2026-11-15", "assignee": "1"}})
	id := res.Target
	if id == "" {
		t.Fatal("no record id was made")
	}
	r := f.record(id, res.Seq)
	if str(r["title"]) != `"Write the plan"` || str(r["done"]) != "false" || str(r["_id"]) != `"`+id+`"` || r.Seq() != res.Seq {
		t.Fatalf("created = %v", r)
	}

	res = f.ok("record.update", id, map[string]any{"entity": "task", "set": map[string]any{"done": true, "title": "Write the plan"}})
	f.ok("record.addTo", id, map[string]any{"entity": "task", "field": "tags", "items": []string{"urgent", "home"}})
	f.ok("record.removeFrom", id, map[string]any{"entity": "task", "field": "tags", "items": []string{"home"}})
	res = f.ok("record.update", id, map[string]any{"entity": "task", "set": map[string]any{"due": nil}})
	r = f.record(id, res.Seq)
	if str(r["done"]) != "true" || str(r["tags"]) != `["urgent"]` || r["due"] != nil {
		t.Errorf("after changes = %v", r)
	}
	res = f.ok("record.delete", id, map[string]any{"entity": "task"})
	if v := f.wait(data.Projector, data.Key("task", id), res.Seq); v.Value != nil {
		t.Errorf("deleted record is %s", v.Value)
	}

	var got []string
	for _, e := range f.events(id) {
		var c data.Change
		_ = json.Unmarshal(e.Data, &c)
		s := e.Type
		switch e.Type {
		case data.EventUpdated:
			s += fmt.Sprintf(" %v←%v", c.Values, c.Old)
		case data.EventAdded, data.EventRemoved:
			s += fmt.Sprintf(" %s %s", c.Field, c.Items)
		case data.EventDeleted:
			s += fmt.Sprintf(" old title %s", c.Old["title"])
		}
		got = append(got, s)
	}
	want := []string{
		"created",
		`updated map[done:true]←map[done:false]`,
		`added tags ["urgent" "home"]`,
		`removed tags ["home"]`,
		`updated map[due:null]←map[due:"2026-11-15"]`,
		`deleted old title "Write the plan"`,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("events =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestValuesAreChecked(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "draft", map[string]any{"name": "Draft"})
	f.rejected("record.create", "", map[string]any{"entity": "draft", "values": map[string]any{}}, "has not been published")
	f.task()

	f.rejected("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"done": true}}, "{ args.values.title title is required")
	f.rejected("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "x", "done": "yes"}}, "args.values.done")
	f.rejected("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "x", "due": "tomorrow"}}, "YYYY-MM-DD")
	f.rejected("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "x", "nope": 1}}, `no field "nope"`)
	res := f.ok("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "x"}})
	f.rejected("record.update", res.Target, map[string]any{"entity": "task", "set": map[string]any{"title": nil}}, "title is required")
	f.rejected("record.addTo", res.Target, map[string]any{"entity": "task", "field": "done", "items": []any{true}}, "not a list")

	// A stale expect is record_changed
	r := f.record(res.Target, res.Seq)
	f.ok("record.update", res.Target, map[string]any{"entity": "task", "set": map[string]any{"done": true}})
	stale := f.rejected("record.update", res.Target, map[string]any{"entity": "task", "set": map[string]any{"done": false}, "expect": r.Seq()}, "changed since")
	if stale.Common.ErrorCode != "record_changed" {
		t.Errorf("stale expect = %s", stale.Common.ErrorCode)
	}

	// A deleted entity type refuses its records' commands until restored
	f.ok("entity.delete", "task", map[string]any{})
	time.Sleep(100 * time.Millisecond)
	f.rejected("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "y"}}, "is deleted")
	f.ok("entity.restore", "task", map[string]any{})
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if res := f.on(f.bus, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "y"}}); res.Common.Code < 300 {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("not restored: %+v", res.Problems)
		}
	}
}

func TestConcurrentChanges(t *testing.T) {
	f := setup(t)
	f.task()
	res := f.ok("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Toggle me"}})
	id := res.Target
	f.record(id, res.Seq)

	// Two connections, each with its own bus, toggle done at the same moment
	a, b := bus.New(f.p), bus.New(f.p)
	var wg sync.WaitGroup
	results := make([]bus.Result, 2)
	for i, x := range []*bus.Bus{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = f.on(x, "record.update", id, map[string]any{"entity": "task", "set": map[string]any{"done": i == 0}})
		}()
	}
	wg.Wait()
	for _, r := range results {
		if r.Common.Code >= 300 {
			t.Fatalf("a toggle failed: %s", r.Common.Message)
		}
	}
	first := results[0].Seq > results[1].Seq // the first connection's toggle came later
	later := results[1]
	if first {
		later = results[0]
	}
	r := f.record(id, later.Seq)
	if want := fmt.Sprint(first); str(r["done"]) != want || r.Seq() != later.Seq {
		t.Errorf("done = %s at %d, want the later toggle's %s at %d", r["done"], r.Seq(), want, later.Seq)
	}

	// Two connections change different fields at once: both are kept
	var title, due bus.Result
	wg.Add(2)
	go func() {
		defer wg.Done()
		title = f.on(a, "record.update", id, map[string]any{"entity": "task", "set": map[string]any{"title": "Renamed"}})
	}()
	go func() {
		defer wg.Done()
		due = f.on(b, "record.update", id, map[string]any{"entity": "task", "set": map[string]any{"due": "2026-12-01"}})
	}()
	wg.Wait()
	r = f.record(id, max(title.Seq, due.Seq))
	if str(r["title"]) != `"Renamed"` || str(r["due"]) != `"2026-12-01"` {
		t.Errorf("after concurrent fields = %v", r)
	}
}

func TestRenameAndTypeChange(t *testing.T) {
	f := setup(t)
	f.task()
	var ids []string
	var last uint64
	for _, p := range []string{"3", "five", "8"} {
		res := f.ok("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "t" + p, "points": p}})
		ids, last = append(ids, res.Target), res.Seq
	}
	f.record(ids[2], last)
	recEvents := func() int {
		s, _ := f.js.Stream(context.Background(), live.StreamName(features.LocalTenantID, live.AreaRecords))
		info, _ := s.Info(context.Background())
		return int(info.State.Msgs)
	}
	n := recEvents()

	// Renaming writes nothing to the records, which stay readable by id
	f.ok("entity.updateField", "task", map[string]any{"field": "title", "name": "summary"})
	f.publish("task", 3)
	if recEvents() != n {
		t.Errorf("a rename wrote %d record events", recEvents()-n)
	}
	list := f.svc.List(recordservice.ListArgs{ServiceArgs: args(), Entity: "task"})
	if len(list.Records) != 3 || str(list.Records[0]["title"]) != `"t3"` {
		t.Errorf("after rename = %v", list.Records)
	}

	// points becomes a number: "five" cannot, and is cleared with a problem
	f.ok("entity.updateField", "task", map[string]any{"field": "points", "type": "number"})
	f.publish("task", 5)
	var got []string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		got = got[:0]
		for _, r := range f.svc.List(recordservice.ListArgs{ServiceArgs: args(), Entity: "task"}).Records {
			got = append(got, fmt.Sprintf("%s:%s", orEmpty(r["points"]), orEmpty(r[data.MetaProblems])))
		}
		if len(got) == 3 && strings.Contains(got[1], "could not") {
			break
		}
	}
	if len(got) != 3 || got[0] != "3:" || got[2] != "8:" || !strings.Contains(got[1], `"points":"\"five\" could not become a number`) {
		t.Errorf("converted = %v", got)
	}
	// A converted value is a number
	if r := f.svc.Get(recordservice.GetArgs{ServiceArgs: args(), Entity: "task", ID: ids[0]}).Record; str(r["points"]) != "3" {
		t.Errorf("points = %s", r["points"])
	}
}

func TestProjectionRebuildsAndImport(t *testing.T) {
	f := setup(t)
	f.task()

	// A second subscriber to records.list sees the import arrive
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &listSink{rows: map[string]bool{}, got: make(chan struct{}, 4096)}
	a := args()
	a.Ctx = ctx
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: a, Query: "records.list", Params: json.RawMessage(`{"entity":"task","where":{"done":false}}`), Sink: s}); fail != nil {
		t.Fatal(fail.Message)
	}

	rows := make([]map[string]json.RawMessage, 1000)
	for i := range rows {
		rows[i] = map[string]json.RawMessage{"title": json.RawMessage(fmt.Sprintf(`"task %d"`, i)), "done": json.RawMessage(fmt.Sprint(i%2 == 1))}
	}
	rows[7] = map[string]json.RawMessage{"done": json.RawMessage("true")}
	imp := f.svc.Import(recordservice.ImportArgs{ServiceArgs: args(), Entity: "task", Rows: rows})
	if imp.Created != 999 || len(imp.Failed) != 1 || imp.Failed[0].Row != 7 {
		t.Fatalf("import = %d created, failed %+v (%s)", imp.Created, imp.Failed, imp.Common.Message)
	}
	for deadline := time.Now().Add(5 * time.Second); s.count() < 500 && time.Now().Before(deadline); {
		select {
		case <-s.got:
		case <-time.After(100 * time.Millisecond):
		}
	}
	if s.count() != 500 {
		t.Errorf("the subscriber saw %d open tasks, want 500", s.count())
	}
	// Toggling one done removes it from the filtered list
	open := imp.IDs[0]
	f.ok("record.update", open, map[string]any{"entity": "task", "set": map[string]any{"done": true}})
	for deadline := time.Now().Add(2 * time.Second); s.count() != 499 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if s.count() != 499 {
		t.Errorf("after toggling, the subscriber has %d", s.count())
	}

	// Acceptance: a projection deleted and rebuilt from the stream equals
	// the one it replaced
	before := f.svc.List(recordservice.ListArgs{ServiceArgs: args(), Entity: "task"}).Records
	f.p.Close()
	bg := context.Background()
	if err := f.js.DeleteKeyValue(bg, live.Bucket(features.LocalTenantID, data.Projector)); err != nil {
		t.Fatal(err)
	}
	stream, _ := f.js.Stream(bg, live.StreamName(features.LocalTenantID, live.AreaRecords))
	if err := stream.DeleteConsumer(bg, "prj_records_record"); err != nil {
		t.Fatal(err)
	}
	p := project.New(f.js, f.reg)
	t.Cleanup(p.Close)
	for _, r := range before {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		if _, err := p.WaitFor(bg, features.LocalTenantID, data.Projector, data.Key("task", id), r.Seq(), 3*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	rebuilt := recordimpl.New(recordimpl.WithRecords(bus.New(p), subscribe.New(p, f.reg), data.Deps{Projections: p}))
	after := rebuilt.List(recordservice.ListArgs{ServiceArgs: args(), Entity: "task"}).Records
	if len(after) != 999 || !reflect.DeepEqual(norm(before), norm(after)) {
		t.Errorf("rebuilt projection differs: %d records before, %d after", len(before), len(after))
	}
}

func orEmpty(v json.RawMessage) string {
	if string(v) == "null" {
		return ""
	}
	return string(v)
}

func norm(rs []data.Record) []string {
	var out []string
	for _, r := range rs {
		raw, _ := json.Marshal(r)
		out = append(out, string(raw))
	}
	return out
}

type listSink struct {
	mu   sync.Mutex
	rows map[string]bool
	got  chan struct{}
}

func (s *listSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}
func (s *listSink) Snapshot(d any, _ live.Position) {
	raw, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	s.mu.Lock()
	s.rows = map[string]bool{}
	for k := range m {
		s.rows[k] = true
	}
	s.mu.Unlock()
}
func (s *listSink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	if d.Op == "remove" {
		delete(s.rows, d.Key)
	} else {
		s.rows[d.Key] = true
	}
	s.mu.Unlock()
	select {
	case s.got <- struct{}{}:
	default:
	}
}
func (s *listSink) Failed(resp features.ServiceResponse) { panic(resp.Message) }
