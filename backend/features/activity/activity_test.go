package activity

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	"github.com/nats-io/nats.go/jetstream"
)

// Forge 15c: the history of what a person does.

const (
	ana uint64 = 2
	bo  uint64 = 3
)

// people is the activity section as each person resolves it.
type people struct {
	mu  sync.Mutex
	set map[uint64]Settings
}

func (p *people) Resolved(args features.ServiceArgs, _ string) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.set[args.Principal.UserID]
	if !ok {
		s = Defaults
	}
	return json.Marshal(s)
}

func (p *people) put(user uint64, s Settings) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.set[user] = s
}

// process is one runner: its projections, bus, store and recorder over
// the shared JetStream.
type process struct {
	p    *project.Projections
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	rec  *Recorder
	deps Deps
	reg  *extensions.Registry
}

type fixture struct {
	t     *testing.T
	js    jetstream.JetStream
	conf  *people
	procs []*process
	n     atomic.Int64
	pages map[string]*ui.Session
}

func newProcess(t *testing.T, js jetstream.JetStream, conf *people) *process {
	reg := extensions.NewRegistry()
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	pr := &process{reg: reg}
	var entityDeps entities.Deps
	var recordDeps data.Deps
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return recordDeps })
	ContributeAccess(reg)
	ContributeSettings(reg)
	pr.rec = NewRecorder(func() Deps { return pr.deps })
	Contribute(reg, pr.rec)
	pr.p = project.New(js, reg)
	t.Cleanup(pr.p.Close)
	pr.bus = bus.New(pr.p)
	pr.subs = subscribe.New(pr.p, reg)
	recordDeps = data.Deps{Projections: pr.p, Bus: pr.bus}
	entityDeps = entities.Deps{Projections: pr.p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, recordDeps) }}
	pr.deps = Deps{Projections: pr.p, Store: NewStore(js), Config: conf, Bus: pr.bus}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := pr.rec.Serve(ctx, 1); err != nil {
		t.Fatal(err)
	}
	return pr
}

func setup(t *testing.T, processes int) *fixture {
	srv := natsd.StartTest(t)
	f := &fixture{t: t, js: srv.JS(), conf: &people{set: map[uint64]Settings{}}, pages: map[string]*ui.Session{}}
	for range processes {
		f.procs = append(f.procs, newProcess(t, f.js, f.conf))
	}
	f.ticket()
	return f
}

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
}

func (f *fixture) exec(args features.ServiceArgs, actor, typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	res := f.procs[0].bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("c%d-%d", time.Now().UnixNano(), f.n.Add(1)), Type: typ, Target: target, Actor: actor, Args: raw})
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

// ticket defines and publishes Ticket: a title, a label and a body.
func (f *fixture) ticket() {
	f.exec(as(1), "", "entity.create", "ticket", map[string]any{"name": "Ticket", "titleField": "title", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "label", "type": "enum", "options": []any{map[string]any{"id": "billing", "name": "Billing"}, map[string]any{"id": "bug", "name": "Bug"}}},
		map[string]any{"name": "body", "type": "markdown"},
	}})
	res := f.exec(as(1), "", "entity.publish", "ticket", map[string]any{"rev": 1})
	if _, err := f.procs[0].p.WaitFor(context.Background(), 1, entities.PublishedProjector, "entity.ticket", res.Seq, 3*time.Second); err != nil {
		f.t.Fatal(err)
	}
}

// commander sends a page's commands on the bus as the UI service does.
type commander struct{ b *bus.Bus }

func (c commander) Execute(args features.ServiceArgs, r ui.CommandRequest) ([]live.Problem, *features.ServiceResponse) {
	res := c.b.Execute(bus.Request{ServiceArgs: args, ID: r.ID, Type: r.Type, Target: r.Target, RequestID: r.Request, App: r.App, Args: r.Args})
	if res.Common.Code >= 300 {
		return res.Problems, &res.Common
	}
	return nil, nil
}

// desk is a page of the app desk on one ticket: a Billing button labels
// it, and writes a body the history must never copy.
func desk(ticket string) ui.Page {
	return ui.Page{
		Descriptor: extensions.Descriptor{ID: "desk/ticket"}, App: "desk", Title: "Desk",
		Subject: func(json.RawMessage) ui.Subject {
			return ui.Subject{Type: "ticket", ID: ticket, Label: "Refund for the second charge?"}
		},
		Render: func(r *ui.Render) *ui.Node {
			return ui.N("mui.stack", "root",
				ui.N("mui.button", "billing").P("label", "Billing").E("click", "label", "secret", "event.secret"),
				ui.N("mui.textField", "search").P("placeholder", "Search").E("change", "search"))
		},
		Handlers: map[string]func(h *ui.Handle) error{
			"label": func(h *ui.Handle) error {
				_, fail := h.Command("record.update", ticket, map[string]any{"entity": "ticket", "set": map[string]any{"label": "billing", "body": "Dear Ana, the refund of the secret amount"}})
				if fail != nil {
					return fmt.Errorf("%s", fail.Message)
				}
				return nil
			},
			"search": func(h *ui.Handle) error { return nil },
		},
	}
}

// open opens the desk page as a person, on process i, with its recorder.
func (f *fixture) open(i int, user uint64, ticket string) *ui.Session {
	f.t.Helper()
	pr := f.procs[i]
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	args := as(user)
	args.Ctx = ctx
	s, fail := ui.Open(args, fmt.Sprintf("s%d", f.n.Add(1)), desk(ticket), json.RawMessage(`{}`), nil, ui.Options{
		Catalog: ui.Catalog(pr.reg), Commander: commander{pr.bus}, Recorder: pr.rec, Send: func(any) {}})
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	f.t.Cleanup(func() { s.Close(true) })
	return s
}

// read is a person's activity, waiting until ok holds of it.
func (f *fixture) read(user uint64, q Params, what string, ok func(Activity) bool) Activity {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := Read(context.Background(), f.procs[0].deps, 1, user, q)
		if err != nil {
			f.t.Fatal(err)
		}
		if ok(a) {
			return a
		}
		if time.Now().After(deadline) {
			raw, _ := json.MarshalIndent(a, "", " ")
			f.t.Fatalf("%s did not happen: %s", what, raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func count(n int) func(Activity) bool {
	return func(a Activity) bool { return len(a.Gestures) == n }
}

// newTicket makes a ticket as the service, which records nothing.
func (f *fixture) newTicket(title string) string {
	res := f.exec(features.ServiceArgs{Ctx: context.Background(), Principal: features.ServicePrincipal(1)}, "", "record.create", "", map[string]any{"entity": "ticket", "values": map[string]any{"title": title}})
	return res.Target
}

func TestAClickAndItsCommandsAreOneGesture(t *testing.T) {
	f := setup(t, 1)
	id := f.newTicket("Refund for the second charge?")
	if err := f.procs[0].rec.Focus(as(ana), "", Thing{Type: "ticket", ID: id, Label: "Refund for the second charge?", Details: map[string]string{"account": "support@"}}); err != nil {
		t.Fatal(err)
	}
	s := f.open(0, ana, id)
	// Typing: a window event and nothing else
	s.Event(as(ana), "window:instances.root.rows", nil, map[string]any{"first": 0, "last": 10})
	s.Event(as(ana), "label", []string{"billing"}, map[string]any{"secret": "the payload's secret"})

	a := f.read(ana, Params{}, "a focus and a click", func(a Activity) bool {
		return len(a.Gestures) == 2 && len(a.Gestures[1].Entries) == 2
	})
	focus, click := a.Gestures[0], a.Gestures[1]
	if focus.Summary != `opened "Refund for the second charge?"` || focus.Entries[0].Kind != KindFocus || focus.Subject.Details["account"] != "support@" {
		t.Errorf("focus = %+v", focus)
	}
	if !strings.HasPrefix(click.Summary, `clicked "Billing" in Desk → `) || !strings.Contains(click.Summary, `"Refund for the second charge?" (body, label: Billing)`) {
		t.Errorf("click summary = %q", click.Summary)
	}
	ui0, cmd := click.Entries[0], click.Entries[1]
	if ui0.Kind != KindUI || ui0.Handler != "label" || ui0.Label != "Billing" || ui0.Event != "click" || ui0.Node != "mui.button" || strings.Join(ui0.Path, "/") != "billing" || ui0.App != "desk" {
		t.Errorf("ui entry = %+v", ui0)
	}
	if cmd.Kind != KindCommand || cmd.Command != "record.update" || cmd.App != "desk" || cmd.Request != ui0.Request || cmd.RecordTitle != "Refund for the second charge?" || cmd.EntityName != "Ticket" {
		t.Errorf("command entry = %+v", cmd)
	}
	if len(cmd.Fields) != 2 || cmd.Fields[0].Name != "body" || !cmd.Fields[0].Omitted || cmd.Fields[1].Value != "Billing" {
		t.Errorf("fields = %+v", cmd.Fields)
	}
	raw, _ := json.Marshal(a)
	for _, never := range []string{"the payload's secret", "secret amount", "window:"} {
		if strings.Contains(string(raw), never) {
			t.Errorf("the history holds %q: %s", never, raw)
		}
	}

	// The same event on the same node again within 30 seconds, with no
	// commands, is one gesture: typing in a search field
	for range 3 {
		s.Event(as(ana), "search", []string{"search"}, map[string]any{"value": "refu"})
	}
	a = f.read(ana, Params{}, "the searches merged", func(a Activity) bool {
		return len(a.Gestures) == 3 && a.Gestures[2].Repeats == 2
	})
	if a.Gestures[2].Summary != `changed "Search" in Desk` {
		t.Errorf("search = %q", a.Gestures[2].Summary)
	}

	// activity.mine gives the same, to Ana
	data, _, fail := f.procs[0].subs.Once(as(ana), "activity.mine", json.RawMessage(`{"limit": 2}`))
	if fail != nil {
		t.Fatal(fail.Message)
	}
	raw, _ = json.Marshal(data)
	var mine Activity
	_ = json.Unmarshal(raw, &mine)
	if !mine.Recording || len(mine.Gestures) != 2 || mine.Gestures[0].ID != click.ID {
		t.Errorf("activity.mine = %s", raw)
	}
}

func TestOnlyThePersonsOwnGestures(t *testing.T) {
	f := setup(t, 1)
	id := f.newTicket("Printer on fire")
	set := func(v string) map[string]any {
		return map[string]any{"entity": "ticket", "set": map[string]any{"label": v}}
	}
	// An agent's run, an app's trigger and the chat's tool call record nothing
	f.exec(features.ServiceArgs{Ctx: context.Background(), Principal: features.AgentPrincipal(1, 77, ana)}, "", "record.update", id, set("bug"))
	f.exec(features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(1, "desk")}, "", "record.update", id, set("bug"))
	f.exec(as(ana), "chat", "record.update", id, set("bug"))
	f.exec(as(ana), "", "record.update", id, set("billing"))
	a := f.read(ana, Params{}, "Ana's own change", count(1))
	if c := a.Gestures[0].Entries[0]; c.Fields[0].Value != "Billing" {
		t.Errorf("recorded %+v", c)
	}

	// Bo sees none of it, and cannot ask for Ana's
	data, _, fail := f.procs[0].subs.Once(as(bo), "activity.mine", nil)
	if fail != nil {
		t.Fatal(fail.Message)
	}
	if raw, _ := json.Marshal(data); strings.Contains(string(raw), "Printer") {
		t.Errorf("Bo reads %s", raw)
	}
	for _, params := range []string{`{"user": "2"}`, `{"principal": "2"}`} {
		if _, _, fail := f.procs[0].subs.Once(as(bo), "activity.mine", json.RawMessage(params)); fail == nil {
			t.Errorf("activity.mine with %s was not refused", params)
		}
	}
	// No agent reads anyone's
	if _, _, fail := f.procs[0].subs.Once(features.ServiceArgs{Ctx: context.Background(), Principal: features.AgentPrincipal(1, 77, ana)}, "activity.mine", nil); fail == nil {
		t.Error("an agent read activity.mine")
	}
}

// call runs one of the activity calls as args, giving its result.
func (f *fixture) call(args features.ServiceArgs, id string, a any) *features.ServiceResponse {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	for _, c := range live.Calls.All(f.procs[0].reg) {
		if c.ID == id {
			return c.Run(live.CallRun{ServiceArgs: args, Args: raw, Emit: func(string, any) {}})
		}
	}
	f.t.Fatalf("no call %s", id)
	return nil
}

func TestDismissAndClear(t *testing.T) {
	f := setup(t, 1)
	for i := range 3 {
		if fail := f.call(as(ana), "activity.focus", map[string]any{"id": fmt.Sprintf("focus-%d", i), "subject": map[string]any{"type": "ticket", "id": fmt.Sprint(i), "label": fmt.Sprintf("Ticket %d", i)}}); fail != nil {
			t.Fatal(fail.Message)
		}
	}
	// A focus sent again is a duplicate
	f.call(as(ana), "activity.focus", map[string]any{"id": "focus-1", "subject": map[string]any{"type": "ticket", "id": "1"}})
	f.read(ana, Params{}, "three focuses", count(3))
	if fail := f.call(as(ana), "activity.dismiss", map[string]any{"ids": []string{"focus-1"}}); fail != nil {
		t.Fatal(fail.Message)
	}
	a := f.read(ana, Params{Sendable: true}, "the dismissed one left out", count(2))
	if a.Gestures[0].ID != "focus-0" || a.Gestures[1].ID != "focus-2" {
		t.Errorf("sendable = %+v", a.Gestures)
	}
	all := f.read(ana, Params{}, "all three", count(3))
	if !all.Gestures[1].Dismissed {
		t.Errorf("not marked dismissed: %+v", all.Gestures[1])
	}
	// Bo cannot dismiss for Ana (there is no way to name her), nor an agent focus
	if fail := f.call(as(bo), "activity.dismiss", map[string]any{"ids": []string{"focus-0"}, "user": "2"}); fail == nil {
		t.Error("a dismiss naming a user was not refused")
	}
	if fail := f.call(features.ServiceArgs{Ctx: context.Background(), Principal: features.AgentPrincipal(1, 77, ana)}, "activity.focus", map[string]any{"subject": map[string]any{"type": "t", "id": "1"}}); fail == nil || fail.Code != 403 {
		t.Errorf("an agent's focus = %+v", fail)
	}
	if fail := f.call(as(ana), "activity.clear", map[string]any{}); fail != nil {
		t.Fatal(fail.Message)
	}
	f.read(ana, Params{}, "cleared", count(0))
	if entries, _ := f.procs[0].deps.Store.Read(context.Background(), 1, ana); len(entries) != 1 || entries[0].Kind != KindClear {
		t.Errorf("after a clear the stream holds %+v", entries)
	}
}

func TestRecordingOffAndTheLimits(t *testing.T) {
	f := setup(t, 1)
	rec, store := f.procs[0].rec, f.procs[0].deps.Store
	focus := func(user uint64, n int) {
		for i := range n {
			if err := rec.Focus(as(user), "", Thing{Type: "ticket", ID: fmt.Sprint(i), Label: "T"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// keepCount
	f.conf.put(ana, Settings{Record: true, KeepHours: 4, KeepCount: 3})
	focus(ana, 5)
	if entries, _ := store.Read(context.Background(), 1, ana); len(entries) != 3 {
		t.Errorf("kept %d entries, not 3", len(entries))
	}
	// record off: nothing is recorded, and what was kept is cleared
	f.conf.put(ana, Settings{Record: false, KeepHours: 4, KeepCount: 3})
	if err := purgeOff(context.Background(), f.procs[0].deps, fmt.Sprintf("user:%d", ana)); err != nil {
		t.Fatal(err)
	}
	if entries, _ := store.Read(context.Background(), 1, ana); len(entries) != 0 {
		t.Errorf("recording off kept %d entries", len(entries))
	}
	focus(ana, 2)
	s := f.open(0, ana, f.newTicket("Off the record"))
	s.Event(as(ana), "label", []string{"billing"}, nil)
	time.Sleep(300 * time.Millisecond)
	if entries, _ := store.Read(context.Background(), 1, ana); len(entries) != 0 {
		t.Errorf("recording off recorded %+v", entries)
	}
	if a, _ := Read(context.Background(), f.procs[0].deps, 1, ana, Params{}); a.Recording || len(a.Gestures) != 0 {
		t.Errorf("activity with recording off = %+v", a)
	}

	// keepHours: an hour is a second here
	defer func(h time.Duration) { hour = h }(hour)
	hour = time.Second
	f.conf.put(bo, Settings{Record: true, KeepHours: 1, KeepCount: 300})
	focus(bo, 2)
	if entries, _ := store.Read(context.Background(), 1, bo); len(entries) != 2 {
		t.Fatalf("Bo has %d entries", len(entries))
	}
	time.Sleep(2500 * time.Millisecond)
	if entries, _ := store.Read(context.Background(), 1, bo); len(entries) != 0 {
		t.Errorf("entries past keepHours are kept: %+v", entries)
	}
}

func TestTwoRunnersWriteEachGestureOnce(t *testing.T) {
	f := setup(t, 2)
	id := f.newTicket("Two runners")
	// A click on a page of each process, and commands both followers see
	s0, s1 := f.open(0, ana, id), f.open(1, ana, id)
	s0.Event(as(ana), "label", []string{"billing"}, nil)
	s1.Event(as(ana), "label", []string{"billing"}, nil)
	// (the second click changes nothing, so it commits nothing)
	for _, v := range []string{"bug", "billing", "bug"} {
		f.exec(as(ana), "", "record.update", id, map[string]any{"entity": "ticket", "set": map[string]any{"label": v}})
	}
	a := f.read(ana, Params{}, "two clicks and three changes", func(a Activity) bool {
		n := 0
		for _, g := range a.Gestures {
			n += len(g.Entries)
		}
		return len(a.Gestures) == 5 && n == 6
	})
	// Both recorders writing one gesture store it once
	g := ui.Gesture{Request: "same-request", Page: "desk/ticket", Handler: "label", Label: "Billing", Time: time.Now().UTC()}
	f.procs[0].rec.Gesture(as(ana), g)
	f.procs[1].rec.Gesture(as(ana), g)
	time.Sleep(300 * time.Millisecond)
	a = f.read(ana, Params{}, "the shared gesture", count(6))
	if last := a.Gestures[5]; last.ID != "same-request" || len(last.Entries) != 1 {
		t.Errorf("shared gesture = %+v", last)
	}
}

func TestHandlerWords(t *testing.T) {
	for in, want := range map[string]string{"hello-onSelect": "select", "onAdd": "add", "mail-onRunAgent": "run agent", "label": "label", "prefab:root/rows#onToggle": "toggle"} {
		if got := handlerWords(in); got != want {
			t.Errorf("handlerWords(%q) = %q, want %q", in, got, want)
		}
	}
}

// Forge 15g: a person marks values of a gesture to ask for each run. The
// last mark of a field wins, a removing mark clears it, only a command's
// fields can be marked, an omitted value has no default, and a clear
// drops the marks with the gestures.
func TestAsksMarkValuesOfAGesture(t *testing.T) {
	now := time.Now().UTC()
	label := Entry{ID: "e1", Kind: KindCommand, Request: "g1", Time: now, Command: "mail.thread.label", Summary: `Label a thread "Invoice" (Labels: Billing)`,
		Fields: []Field{{ID: "labels", Name: "Labels", Value: []any{"Billing"}}, {ID: "body", Name: "Body", Omitted: true}}}
	click := Entry{ID: "u1", Kind: KindUI, Request: "g1", Time: now, Label: "Labels", Summary: `changed "Labels"`}
	mark := func(m Mark) Entry { return Entry{ID: "a-" + m.Field + m.Question, Kind: KindAsk, Time: now, Ask: &m} }
	group := func(es ...Entry) Gesture {
		t.Helper()
		a := Group(es, Defaults, now.Add(time.Second), Params{})
		if len(a.Gestures) != 1 {
			t.Fatalf("%d gestures", len(a.Gestures))
		}
		return a.Gestures[0]
	}

	g := group(click, label, mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels"}))
	if len(g.Asks) != 1 || g.Asks[0].Name != "Labels" || g.Asks[0].Question != "Which labels?" || fmt.Sprint(g.Asks[0].Default) != "[Billing]" {
		t.Fatalf("a mark with no question: %+v", g.Asks)
	}
	g = group(click, label, mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels", Question: "Which label?"}), mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels", Question: "Which label, then?", Default: "Invoices"}),
		mark(Mark{Gesture: "g1", Entry: "e1", Field: "body", Question: "What reply?"}))
	if len(g.Asks) != 2 || g.Asks[0].Question != "Which label, then?" || g.Asks[0].Default != "Invoices" || !g.Asks[1].Omitted || g.Asks[1].Default != nil {
		t.Fatalf("the last mark wins, an omitted value has no default: %+v", g.Asks)
	}
	g = group(click, label, mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels"}), mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels", Remove: true}),
		mark(Mark{Gesture: "g1", Entry: "u1", Field: "labels"}), mark(Mark{Gesture: "g2", Entry: "e1", Field: "labels"}), mark(Mark{Gesture: "g1", Entry: "e1", Field: "nope"}))
	if len(g.Asks) != 0 {
		t.Fatalf("removed, a click's, another gesture's and an unknown field's marks: %+v", g.Asks)
	}
	a := Group([]Entry{label, mark(Mark{Gesture: "g1", Entry: "e1", Field: "labels"}), {ID: "c", Kind: KindClear, Time: now}, label}, Defaults, now.Add(time.Second), Params{})
	if len(a.Gestures) != 1 || len(a.Gestures[0].Asks) != 0 {
		t.Fatalf("a clear drops the marks: %+v", a.Gestures)
	}
}
