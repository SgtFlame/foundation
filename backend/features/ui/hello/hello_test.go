package hello_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/services/liveservice"
	liveimpl "github.com/SgtFlame/foundation/backend/features/live/services/liveservice/impl"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	"github.com/SgtFlame/foundation/backend/features/ui/hello"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"github.com/SgtFlame/foundation/backend/features/ui/rest/uihandlers"
	"github.com/SgtFlame/foundation/backend/features/ui/services/uiservice"
	uiimpl "github.com/SgtFlame/foundation/backend/features/ui/services/uiservice/impl"
)

// socket is an in-memory live connection.
type socket struct {
	in  chan []byte
	out chan []byte
}

func (s *socket) Read(ctx context.Context) (bool, []byte, error) {
	select {
	case m, ok := <-s.in:
		if !ok {
			return false, nil, errors.New("closed")
		}
		return false, m, nil
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}

func (s *socket) Write(ctx context.Context, binary bool, msg []byte) error {
	select {
	case s.out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type client struct {
	t *testing.T
	s *socket
}

func (c *client) send(frame string) { c.s.in <- []byte(frame) }

// await reads frames until one contains all of want.
func (c *client) await(want ...string) string {
	c.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-c.s.out:
			ok := true
			for _, w := range want {
				ok = ok && strings.Contains(string(m), w)
			}
			if ok {
				return string(m)
			}
		case <-timeout:
			c.t.Fatalf("no frame with %v", want)
		}
	}
}

type stack struct {
	t    *testing.T
	bus  *bus.Bus
	p    *project.Projections
	subs *subscribe.Subscriptions
	live liveservice.Service
	n    int
	seq  map[string]uint64
}

func newStack(t *testing.T) *stack {
	reg := extensions.NewRegistry()
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	cognitionnodes.Contribute(reg)
	var draftDeps drafts.Deps
	var liveDeps livenodes.Deps
	var uiDeps uinodes.Deps
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	livenodes.Contribute(reg, func() livenodes.Deps { return liveDeps })
	uinodes.Contribute(reg)
	ui.ContributeOverlays(reg)
	hello.Contribute(reg)
	// The task list's records
	var entityDeps entities.Deps
	var recordDeps data.Deps
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return recordDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return recordDeps })
	// Task row and Task list
	var prefabDeps prefabs.Deps
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), ui.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	pres := presence.New(srv.JS(), nil)
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	liveDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p, Presence: pres}
	uiDeps = uinodes.Deps{Engine: eng, Presence: pres}
	recordDeps = data.Deps{Projections: p, Bus: b, Users: func(_ uint64, ids []uint64) map[uint64]data.User {
		out := map[uint64]data.User{}
		for _, id := range ids {
			out[id] = data.User{Username: fmt.Sprintf("user%d", id), DisplayName: map[uint64]string{1: "Ada", 2: "Bob"}[id]}
		}
		return out
	}}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	entityDeps = entities.Deps{Projections: p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, recordDeps) }}
	b.Resolve = livenodes.CommandFor(func() livenodes.Deps { return liveDeps })
	subs.Resolve = livenodes.QueryFor(func() livenodes.Deps { return liveDeps })
	var uiService uiservice.Service = uiimpl.New(uiimpl.WithRegistry(reg), uiimpl.WithBus(b), uiimpl.WithSubscriptions(subs), uiimpl.WithStore(ui.NewKVSessions(srv.JS())),
		uiimpl.WithGraphs(uinodes.Runner{Deps: func() uinodes.Deps { return uiDeps }}))
	uihandlers.ContributeLive(reg, func() uiservice.Service { return uiService })
	presence.Contribute(reg, func() *presence.Presence { return pres })
	return &stack{t: t, bus: b, p: p, subs: subs, seq: map[string]uint64{},
		live: liveimpl.New(liveimpl.WithBus(b), liveimpl.WithSubscriptions(subs), liveimpl.WithRegistry(reg), liveimpl.WithPresence(pres))}
}

func (st *stack) connect(user uint64) *client {
	s := &socket{in: make(chan []byte, 16), out: make(chan []byte, 1024)}
	ctx, cancel := context.WithCancel(context.Background())
	st.t.Cleanup(cancel)
	go st.live.Serve(liveservice.ServeArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}, Socket: s})
	c := &client{t: st.t, s: s}
	c.await(`"type":"hello"`)
	return c
}

func userArgs() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: 1, TenantID: 1}}
}

// do sends a command as user 1, as a chat's tool calls do, and fails on a rejection.
func (st *stack) do(typ, target string, args any) {
	st.t.Helper()
	st.n++
	raw, _ := json.Marshal(args)
	res := st.bus.Execute(bus.Request{ServiceArgs: userArgs(), ID: fmt.Sprintf("t%d", st.n), Type: typ, Target: target, Args: raw})
	if res.Common.Code >= 300 {
		st.t.Fatalf("%s %s: %s %+v", typ, target, res.Common.Message, res.Problems)
	}
	st.seq[target] = max(st.seq[target], res.Seq)
}

// publish publishes a draft at the rev it is at, and waits until it runs.
func (st *stack) publish(id string) {
	st.t.Helper()
	ctx := context.Background()
	v, err := st.p.WaitFor(ctx, 1, drafts.DraftProjector, "graph."+id, st.seq[id], 2*time.Second)
	if err != nil {
		st.t.Fatal(err)
	}
	var d drafts.Draft
	_ = json.Unmarshal(v.Value, &d)
	st.do("bp.publish", id, map[string]int{"rev": d.Rev})
	if _, err := st.p.WaitFor(ctx, 1, drafts.PublishedProjector, "graph."+id, st.seq[id], 2*time.Second); err != nil {
		st.t.Fatal(err)
	}
}

func pin(node, p string) map[string]string { return map[string]string{"node": node, "pin": p} }

func TestTwoUsersSeeEachOthersIncrements(t *testing.T) {
	st := newStack(t)
	a, bb := st.connect(1), st.connect(2)
	a.send(`{"type":"ui.open","session":"sa","page":"hello/main"}`)
	bb.send(`{"type":"ui.open","session":"sb","page":"hello/main"}`)
	tree := a.await(`"type":"ui.tree"`, `"session":"sa"`)
	bb.await(`"type":"ui.tree"`, `"session":"sb"`)
	if strings.Contains(tree, "ui.error") {
		t.Fatalf("the page has invalid nodes: %s", tree)
	}

	// The increment handler and command are graphs; the counter's query too
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onIncrement","path":["top","counter","row","increment"]}`)
	// Both see the counter's bound prop patched, the sender by the same path
	got := a.await(`"type":"ui.patch"`, `"session":"sa"`, `"text":1`)
	bb.await(`"type":"ui.patch"`, `"session":"sb"`, `"text":1`)
	var pf ui.PatchFrame
	_ = json.Unmarshal([]byte(got), &pf)
	if len(pf.Patches) != 1 || pf.Patches[0].Op != "props" {
		t.Fatalf("want one props patch, got %s", got)
	}

	// Commands straight over the connection, acknowledged with their sequence
	bb.send(`{"type":"command","id":"c1","command":"hello.connect","target":"main","args":{"source":"start","sourceHandle":"out","target":"greet","targetHandle":"in"}}`)
	bb.await(`"type":"ack"`, `"id":"c1"`)
	a.await(`"type":"ui.patch"`, `"edges"`, `start.out-greet.in`)
	bb.send(`{"type":"command","id":"c1","command":"hello.connect","target":"main","args":{"source":"start","sourceHandle":"out","target":"greet","targetHandle":"in"}}`)
	// Resent, it applies once: the edge is there, so nothing more is committed
	bb.await(`"type":"ack"`, `"id":"c1"`)
	bb.send(`{"type":"command","id":"c0","command":"hello.increment","target":"main","args":{"by":2}}`)
	bb.await(`"type":"ack"`, `"id":"c0"`)
	a.await(`"type":"ui.patch"`, `"text":3`)
	bb.send(`{"type":"command","id":"c0","command":"hello.increment","target":"main"}`)
	bb.await(`"type":"ack"`, `"id":"c0"`, `"duplicate":true`)
	bb.send(`{"type":"command","id":"c2","command":"hello.moveNode","target":"main","args":{"id":"nope","position":{"x":1,"y":2}}}`)
	bb.await(`"type":"rejected"`, `"id":"c2"`, `there is no node`)

	// The On connect handler branches: a node joined to itself is a toast
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onConnect","path":["tabs","graph"],"payload":{"source":"greet","sourceHandle":"out","target":"greet","targetHandle":"in"}}`)
	a.await(`"type":"ui.patch"`, `"toast"`, `cannot be joined to itself`)

	// A subscription and presence
	a.send(`{"type":"subscribe","sub":"p","query":"live.presence","params":{"object":"app.hello"}}`)
	a.await(`"type":"snapshot"`, `"sub":"p"`)
	bb.send(`{"type":"presence","object":"app.hello","state":{"selected":["greet"]}}`)
	a.await(`"type":"change"`, `"sub":"p"`, `greet`)

	// The view state of a session survives its connection
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onGreet","path":["top","greet","form","greet"],"payload":{"name":"Ada"}}`)
	a.await(`Hello, Ada!`)
	c := st.connect(1)
	c.send(`{"type":"ui.open","session":"sa","page":"hello/main"}`)
	c.await(`"type":"ui.tree"`, `Hello, Ada!`)
}

// The acceptance of 02-Blueprints through commands alone, as the chat's
// tools send them: a reset button, the command and the handler it needs,
// published, working in an open tab without a reload.
func TestAResetButtonMadeOfBlueprints(t *testing.T) {
	st := newStack(t)
	a := st.connect(1)
	a.send(`{"type":"ui.open","session":"sa","page":"hello/main"}`)
	a.await(`"type":"ui.tree"`, `"session":"sa"`)
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onIncrement","path":["top","counter","row","increment"]}`)
	a.await(`"type":"ui.patch"`, `"text":1`)

	// The command: hello.reset subtracts the counter's value
	st.do("bp.create", "cmd-hello-reset", map[string]any{"kind": "command", "name": "hello.reset", "owner": "app:hello", "spec": map[string]any{
		"command": "hello.reset", "title": "Reset the counter", "description": "Sets a Hello counter to zero.",
		"area": "app_hello", "targetType": "counter", "projection": "hello.counter", "action": "hello.write", "events": []string{"incremented"}}})
	st.do("bp.addNode", "cmd-hello-reset", map[string]any{"type": "data.expression", "id": "by", "config": map[string]any{"expr": "{by: -(state?.value ?? 0)}", "type": "json"}})
	st.do("bp.addNode", "cmd-hello-reset", map[string]any{"type": "live.emit", "id": "emit", "config": map[string]any{"event": "incremented"}})
	st.do("bp.connect", "cmd-hello-reset", map[string]any{"from": pin("event", "then"), "to": pin("emit", "then")})
	st.do("bp.connect", "cmd-hello-reset", map[string]any{"from": pin("event", "state"), "to": pin("by", "state")})
	st.do("bp.connect", "cmd-hello-reset", map[string]any{"from": pin("by", "value"), "to": pin("emit", "data")})
	st.publish("cmd-hello-reset")

	// The handler: send it, then toast
	st.do("bp.create", "hello-onReset", map[string]any{"kind": "ui.handler", "name": "On reset", "owner": "app:hello"})
	st.do("bp.addNode", "hello-onReset", map[string]any{"type": "live.sendCommand", "id": "send", "config": map[string]any{"command": "hello.reset", "target": "main"}})
	st.do("bp.addNode", "hello-onReset", map[string]any{"type": "ui.toast", "id": "toast", "config": map[string]any{"severity": "success", "message": "Counter reset"}})
	st.do("bp.connect", "hello-onReset", map[string]any{"from": pin("event", "then"), "to": pin("send", "then")})
	st.do("bp.connect", "hello-onReset", map[string]any{"from": pin("send", "ok"), "to": pin("toast", "then")})
	st.publish("hello-onReset")

	// The button, added to the open page
	st.do("ui.insertNode", "hello-main", map[string]any{"parent": "root/top/counter/row", "node": map[string]any{
		"type": "mui.button", "key": "reset", "props": map[string]any{"label": "Reset"}, "on": map[string]any{"click": map[string]any{"handler": "hello-onReset"}}}})
	a.await(`"type":"ui.patch"`, `"label":"Reset"`)

	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onReset","path":["top","counter","row","reset"]}`)
	a.await(`"type":"ui.patch"`, `Counter reset`)
	a.await(`"type":"ui.patch"`, `"text":0`)

	// Published again, the handler changes at the next event, in the same tab
	st.do("bp.setConfig", "hello-onReset", map[string]any{"node": "toast", "config": map[string]any{"severity": "success", "message": "Back to zero"}})
	st.publish("hello-onReset")
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onReset","path":["top","counter","row","reset"]}`)
	a.await(`"type":"ui.patch"`, `Back to zero`)

	// A failing handler is reported with the node it failed on
	st.do("bp.create", "hello-onBroken", map[string]any{"kind": "ui.handler", "name": "Broken"})
	st.do("bp.addNode", "hello-onBroken", map[string]any{"type": "data.expression", "id": "boom", "config": map[string]any{"expr": "1 / zero"}})
	st.do("bp.addNode", "hello-onBroken", map[string]any{"type": "ui.setState", "id": "set", "title": "Store it", "config": map[string]any{"path": "x"}})
	st.do("bp.connect", "hello-onBroken", map[string]any{"from": pin("event", "then"), "to": pin("set", "then")})
	st.do("bp.connect", "hello-onBroken", map[string]any{"from": pin("boom", "value"), "to": pin("set", "value")})
	st.publish("hello-onBroken")
	a.send(`{"type":"ui.event","session":"sa","handler":"hello-onBroken","path":["top","counter","row","reset"]}`)
	a.await(`"type":"ui.error"`, `hello-onBroken`, `Expression failed`)
}

func TestTheGraphsAreValid(t *testing.T) {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	livenodes.Contribute(reg, func() livenodes.Deps { return livenodes.Deps{} })
	uinodes.Contribute(reg)
	recordsnodes.Contribute(reg, func() data.Deps { return data.Deps{} })
	hello.Contribute(reg)
	cat := cognition.CatalogOf(reg)
	for _, t2 := range cat.Types {
		if missing := cognition.MetaProblems(t2); len(missing) > 0 {
			t.Errorf("node type %s lacks %v", t2.ID, missing)
		}
	}
	n := 0
	for _, b := range cognition.Graphs.All(reg) {
		n++
		if res := cat.Resolve(b.Graph); len(res.Problems) > 0 {
			t.Errorf("%s: %+v", b.ID, res.Problems)
		}
	}
	if n != 23 {
		t.Errorf("%d graphs, want 23", n)
	}
}
