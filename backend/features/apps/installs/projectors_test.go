package installs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// byAssignee is a projector graph counting tasks made per assignee, each
// counted times weight, so a test tells generations apart.
func byAssignee(weight int) map[string]any {
	return map[string]any{"name": "Tasks per assignee", "kind": livenodes.KindProjector, "owner": "app:tasks",
		"nodes": []any{
			node("event", livenodes.OnFold, nil),
			node("next", "data.expression", map[string]any{"type": "json",
				"expr": fmt.Sprintf(`{count: (state?.count ?? 0) + (event.type == "created" ? %d : 0), last: event.id}`, weight)}),
			node("ret", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "value", "type": "json"}}}),
		},
		"edges": edges("event.then -> ret.then", "event.state -> next.state", "event.event -> next.event", "next.value -> ret.value"),
	}
}

// tasksWithProjector is the Tasks app with the byAssignee projector and
// a typing topic, installed.
func (f *fixture) tasksWithProjector(p *project.Projections) {
	f.t.Helper()
	seq := f.tasks(true)
	f.publishGraph("prj-tasks-byAssignee", byAssignee(1))
	f.ok("app.addProjector", "tasks", map[string]any{"name": "byAssignee", "entities": []string{"task"}, "key": "record.assignee", "graph": "prj-tasks-byAssignee", "description": "Tasks made per assignee: {count, last}"})
	seq = f.ok("app.addEphemeral", "tasks", map[string]any{"topic": "typing", "read": "entity:task.read", "description": "Who is typing in a task"}).Seq
	f.publish(seq)
	f.lifecycle("app.install", "tasks", map[string]any{})
}

func (f *fixture) count(p *project.Projections, key string) int {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id := apps.ProjectorID("tasks", "byAssignee")
	if err := p.Settle(ctx, features.LocalTenantID, id); err != nil {
		f.t.Fatalf("settle: %v", err)
	}
	v, ok, err := p.Get(ctx, features.LocalTenantID, id, key)
	if err != nil {
		f.t.Fatalf("get %s: %v", key, err)
	}
	if !ok {
		return 0
	}
	var c struct{ Count int }
	_ = json.Unmarshal(v.Value, &c)
	return c.Count
}

func TestAppProjector(t *testing.T) {
	f := setup(t)
	f.p.SetResolver(installs.ProjectorResolver(func() installs.Deps { return *f.install }))
	f.tasksWithProjector(f.p)
	for i, who := range []string{"ann", "ann", "bob"} {
		f.ok("record.create", fmt.Sprintf("t%d", i), map[string]any{"entity": "task", "values": map[string]any{"title": "x", "assignee": who}})
	}
	if got := f.count(f.p, "ann"); got != 2 {
		t.Errorf("ann's tasks: %d", got)
	}
	if got := f.count(f.p, "bob"); got != 1 {
		t.Errorf("bob's tasks: %d", got)
	}
	// An update folds, but counts nothing; an unknown projector is refused
	f.ok("record.update", "t2", map[string]any{"entity": "task", "set": map[string]any{"title": "y"}})
	if got := f.count(f.p, "bob"); got != 1 {
		t.Errorf("bob's tasks after an update: %d", got)
	}
	if _, _, err := f.p.Get(context.Background(), features.LocalTenantID, apps.ProjectorID("tasks", "nope"), "ann"); err == nil {
		t.Error("an undeclared projector was read")
	}

	// A second process (its own projections over the same JetStream)
	// shares the consumer: each event is folded once
	p2 := project.New(f.p.JetStream(), f.reg)
	t.Cleanup(p2.Close)
	p2.SetResolver(installs.ProjectorResolver(func() installs.Deps { d := *f.install; d.Projections = p2; return d }))
	_ = f.count(p2, "ann")
	for i := range 6 {
		f.ok("record.create", fmt.Sprintf("u%d", i), map[string]any{"entity": "task", "values": map[string]any{"title": "x", "assignee": "cy"}})
	}
	if got, got2 := f.count(f.p, "cy"), f.count(p2, "cy"); got != 6 || got2 != 6 {
		t.Errorf("two processes folded cy's tasks to %d and %d, not 6", got, got2)
	}

	// A restart resumes where the consumer stopped
	p2.Close()
	p3 := project.New(f.p.JetStream(), f.reg)
	t.Cleanup(p3.Close)
	p3.SetResolver(installs.ProjectorResolver(func() installs.Deps { d := *f.install; d.Projections = p3; return d }))
	if got := f.count(p3, "ann"); got != 2 {
		t.Errorf("after a restart ann's tasks: %d", got)
	}

	// An upgrade with a new graph rebuilds a new generation by replay
	f.publishGraph("prj-tasks-byAssignee", byAssignee(10))
	seq := f.ok("app.updateProjector", "tasks", map[string]any{"name": "byAssignee", "description": "Tasks made per assignee, weighted"}).Seq
	f.publish(seq)
	f.lifecycle("app.upgrade", "tasks", map[string]any{})
	if got := f.count(p3, "ann"); got != 20 {
		t.Errorf("after the upgrade ann's tasks count %d, not rebuilt by the new graph", got)
	}
	if got := f.count(p3, "cy"); got != 60 {
		t.Errorf("after the upgrade cy's tasks count %d", got)
	}
}

// eph collects an ephemeral subscription's result.
type eph struct {
	mu     sync.Mutex
	data   any
	failed string
	got    chan struct{}
}

func (s *eph) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	var v any
	_ = json.Unmarshal(raw, &v)
	s.mu.Lock()
	s.data = v
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *eph) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	s.data = live.Apply(s.data, d)
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *eph) Failed(resp features.ServiceResponse) {
	s.mu.Lock()
	s.failed = resp.Message
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *eph) senders() map[string]any {
	m, _ := s.data.(map[string]any)
	return m
}
func (s *eph) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		done := ok()
		s.mu.Unlock()
		if done {
			return
		}
		select {
		case <-s.got:
		case <-deadline:
			t.Fatalf("no %s arrived", what)
		}
	}
}

func TestEphemeral(t *testing.T) {
	f := setup(t)
	f.tasksWithProjector(f.p)
	// Only a task's assignee reads it
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "x", "assignee": "7"}})
	ann := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: features.LocalTenantID}
	bob := features.Principal{Kind: features.PrincipalUser, UserID: 8, TenantID: features.LocalTenantID}
	f.ok("entity.setAccess", "task", map[string]any{"access": map[string]any{"read": "record.assignee == principal.user"}})
	res := f.ok("entity.publish", "task", map[string]any{"rev": 3})
	f.wait(entities.PublishedProjector, "entity.task", res.Seq)

	open := func(p features.Principal, key string) *eph {
		s := &eph{got: make(chan struct{}, 64)}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		raw, _ := json.Marshal(map[string]any{"app": "tasks", "topic": "typing", "key": key})
		if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: p}, Query: installs.EphemeralQuery, Params: raw, Sink: s}); fail != nil {
			s.failed = fail.Message
		}
		return s
	}
	a := open(ann, "t1")
	a.until(t, "a snapshot", func() bool { return a.data != nil || a.failed != "" })
	if a.failed != "" {
		t.Fatalf("the assignee cannot follow the task's typing: %s", a.failed)
	}
	b := open(bob, "t1")
	b.until(t, "a refusal", func() bool { return b.failed != "" || b.data != nil })
	if b.failed == "" {
		t.Error("someone who cannot read the task follows its typing")
	}
	// bob may not publish on it either; ann may
	asOf := func(p features.Principal) features.ServiceArgs {
		return features.ServiceArgs{Ctx: context.Background(), Principal: p}
	}
	if fail := installs.PublishEphemeral(asOf(bob), *f.install, "tasks", "typing", "t1", true, 0); fail == nil {
		t.Error("someone who cannot read the task published on it")
	}
	if fail := installs.PublishEphemeral(asOf(ann), *f.install, "tasks", "nope", "t1", true, 0); fail == nil {
		t.Error("an undeclared topic was published")
	}
	if fail := installs.PublishEphemeral(asOf(ann), *f.install, "tasks", "typing", "t1", "draft…", time.Second); fail != nil {
		t.Fatalf("publish: %s", fail.Message)
	}
	a.until(t, "the signal", func() bool { s, _ := a.senders()["7"].(map[string]any); return s != nil && s["value"] == "draft…" })
	// It is gone after its time to live
	a.until(t, "the signal's expiry", func() bool { return len(a.senders()) == 0 })
}
