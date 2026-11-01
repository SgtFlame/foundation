package entities_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"gorm.io/gorm"
)

type fixture struct {
	t    *testing.T
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	p    *project.Projections
	db   *gorm.DB
	n    int
	seq  map[string]uint64 // each entity type's last commit
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	var deps entities.Deps
	entities.Contribute(reg, func() entities.Deps { return deps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, records.Models()...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	deps = entities.Deps{Projections: p, DB: gdb}
	return &fixture{t: t, bus: bus.New(p), subs: subscribe.New(p, reg), p: p, db: gdb, seq: map[string]uint64{}}
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) do(typ, target string, a any) bus.Result {
	f.n++
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n), Type: typ, Target: target, Args: raw})
	if res.Common.Code < 300 {
		f.seq[target] = max(f.seq[target], res.Seq)
	}
	return res
}

func (f *fixture) ok(typ, target string, a any) {
	f.t.Helper()
	if res := f.do(typ, target, a); res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
}

// rejected sends a command that must be refused with a problem whose
// message holds want.
func (f *fixture) rejected(typ, target string, a any, want string) {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code != 422 || !strings.Contains(fmt.Sprint(res.Problems), want) {
		f.t.Errorf("%s was not refused with %q: %d %+v", typ, want, res.Common.Code, res.Problems)
	}
}

// describe reads an entity type's draft once its last commit is projected.
func (f *fixture) describe(id string) entities.View {
	f.t.Helper()
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, entities.DraftProjector, "entity."+id, f.seq[id], 2*time.Second); err != nil {
		f.t.Fatal(err)
	}
	data, _, fail := f.subs.Once(args(), "entity.describe", json.RawMessage(`{"id":"`+id+`"}`))
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	var v entities.View
	_ = json.Unmarshal(raw, &v)
	return v
}

func ids(e records.EntityType) string {
	var out []string
	for _, f := range e.Fields {
		s := f.ID + ":" + f.Name
		if f.Hidden {
			s += "(hidden)"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// sink collects a subscription's views.
type sink struct {
	mu    sync.Mutex
	views []entities.View
	got   chan struct{}
}

func (s *sink) add(raw []byte) {
	var v entities.View
	_ = json.Unmarshal(raw, &v)
	s.mu.Lock()
	s.views = append(s.views, v)
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *sink) Snapshot(data any, _ live.Position) { raw, _ := json.Marshal(data); s.add(raw) }
func (s *sink) Change(d live.Delta, _ live.Position) {
	if d.Op == "replace" {
		s.add(d.Value)
	}
}
func (s *sink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

// next waits for the subscription's next view.
func (s *sink) next(t *testing.T) entities.View {
	t.Helper()
	select {
	case <-s.got:
	case <-time.After(3 * time.Second):
		t.Fatal("no change arrived")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.views[len(s.views)-1]
}

func TestTaskIsDefinedRenamedAndPublished(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "task", map[string]any{"name": "Task", "owner": "app:hello"})
	f.describe("task")

	// A second user has the definition open
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &sink{got: make(chan struct{}, 64)}
	a := args()
	a.Ctx = ctx
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: a, Query: "entity.describe", Params: json.RawMessage(`{"id":"task"}`), Sink: s}); fail != nil {
		t.Fatal(fail.Message)
	}
	if v := s.next(t); v.Entity.Name != "Task" || len(v.Entity.Fields) != 0 {
		t.Fatalf("snapshot = %+v", v)
	}

	f.ok("entity.addField", "task", map[string]any{"name": "title", "type": "text", "required": true})
	f.ok("entity.addField", "task", map[string]any{"name": "done", "type": "bool"})
	f.ok("entity.addField", "task", map[string]any{"name": "due", "type": "date"})
	f.ok("entity.addField", "task", map[string]any{"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}})
	v := f.describe("task")
	if got := ids(v.Entity); got != "title:title done:done due:due assignee:assignee" || v.Entity.TitleField != "title" || !v.Entity.Fields[0].Required {
		t.Fatalf("task = %s, title %q", got, v.Entity.TitleField)
	}

	// The rename keeps the id, and the second subscriber sees it land
	f.ok("entity.updateField", "task", map[string]any{"field": "title", "name": "summary"})
	var seen entities.View
	for range 8 {
		if seen = s.next(t); seen.Rev == v.Rev+1 {
			break
		}
	}
	if seen.Entity.Fields[0].ID != "title" || seen.Entity.Fields[0].Name != "summary" || !seen.Entity.Fields[0].Required {
		t.Errorf("the subscriber saw %s", ids(seen.Entity))
	}

	// Removing hides the field; restoring brings it back with the same id
	f.ok("entity.removeField", "task", map[string]any{"field": "due"})
	if v := f.describe("task"); !v.Entity.Fields[2].Hidden || len(v.Entity.Visible()) != 3 {
		t.Errorf("after remove: %s", ids(v.Entity))
	}
	f.ok("entity.restoreField", "task", map[string]any{"field": "due"})
	v = f.describe("task")
	if got := ids(v.Entity); got != "title:summary done:done due:due assignee:assignee" {
		t.Errorf("after restore: %s", got)
	}

	// Publishing what was not seen is refused; a version lands in SQLite
	f.rejected("entity.publish", "task", map[string]any{"rev": v.Rev - 1}, fmt.Sprintf("rev %d", v.Rev))
	f.ok("entity.publish", "task", map[string]any{"rev": v.Rev})
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, entities.PublishedProjector, "entity.task", f.seq["task"], 2*time.Second); err != nil {
		t.Fatal(err)
	}
	data, _, fail := f.subs.Once(args(), "entity.describe", json.RawMessage(`{"id":"task","published":true}`))
	if fail != nil {
		t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	var pub entities.View
	_ = json.Unmarshal(raw, &pub)
	if pub.Published != 1 || pub.Entity.Version != 1 || len(pub.Entity.Fields) != 4 {
		t.Errorf("published = %+v", pub)
	}
	var row records.EntityTypeVersion
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if f.db.Where("tenant_id = ? AND entity_id = ?", features.LocalTenantID, "task").First(&row).Error == nil {
			break
		}
	}
	var def records.EntityType
	_ = json.Unmarshal([]byte(row.Definition), &def)
	if row.Version != 1 || def.Fields[0].Name != "summary" || row.Seq != f.seq["task"] {
		t.Errorf("version row = %+v", row)
	}
	if v := f.describe("task"); v.Published != 1 || v.Changed {
		t.Errorf("draft after publishing = published %d, changed %v", v.Published, v.Changed)
	}
}

func TestInvalidDefinitionsAreRejected(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "task", map[string]any{"name": "Task", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "Due date", "type": "date"},
	}})
	if v := f.describe("task"); ids(v.Entity) != "title:title due_date:Due date" || v.Entity.TitleField != "title" {
		t.Fatalf("task = %s, title %q", ids(v.Entity), v.Entity.TitleField)
	}
	f.rejected("entity.addField", "task", map[string]any{"name": "Title", "type": "text"}, "already a field named")
	f.rejected("entity.addField", "task", map[string]any{"name": "project", "type": "relation", "relation": map[string]any{"entity": "project"}}, `no entity type "project"`)
	f.rejected("entity.setInfo", "task", map[string]any{"titleField": "nope"}, `no field "nope"`)
	f.rejected("entity.addField", "task", map[string]any{"name": "colour", "type": "enum"}, "needs options")
	f.rejected("entity.addField", "task", map[string]any{"name": "x", "type": "nope"}, `no field type "nope"`)
	f.rejected("entity.updateField", "task", map[string]any{"field": "title", "type": "longText"}, "cannot change")
	f.rejected("entity.removeField", "task", map[string]any{"field": "title"}, "title field")
	f.rejected("entity.addField", "nothing", map[string]any{"name": "x", "type": "text"}, `no entity type "nothing"`)

	// A removed field's name is free; restoring it then clashes
	f.ok("entity.removeField", "task", map[string]any{"field": "due_date"})
	f.ok("entity.addField", "task", map[string]any{"name": "due date", "type": "dateTime"})
	if v := f.describe("task"); ids(v.Entity) != "title:title due_date:Due date(hidden) due_date_2:due date" {
		t.Errorf("task = %s", ids(v.Entity))
	}
	f.rejected("entity.restoreField", "task", map[string]any{"field": "due_date"}, "already a field named")

	// An index on a field keeps it from being removed
	f.ok("entity.addIndex", "task", map[string]any{"fields": []string{"due_date_2"}})
	f.rejected("entity.removeField", "task", map[string]any{"field": "due_date_2"}, "not a field")
	f.ok("entity.removeIndex", "task", map[string]any{"index": "by_due_date_2"})
	f.ok("entity.removeField", "task", map[string]any{"field": "due_date_2"})

	// A relation to a known entity type is fine
	f.ok("entity.create", "project", map[string]any{"name": "Project"})
	f.describe("project")
	f.ok("entity.addField", "task", map[string]any{"name": "project", "type": "relation", "relation": map[string]any{"entity": "project"}})

	var list map[string]entities.Summary
	f.describe("task")
	data, _, _ := f.subs.Once(args(), "entity.list", json.RawMessage(`{}`))
	raw, _ := json.Marshal(data)
	_ = json.Unmarshal(raw, &list)
	if len(list) != 2 || list["entity.task"].Fields != 2 || list["entity.project"].Name != "Project" {
		t.Errorf("list = %+v", list)
	}
}
