package connections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/connectors/scripted"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

// Scripted connectors (forge 07d): the Stub app implements test.echo@1 in
// blueprints as the type stub.echo.

type m = map[string]any

// graph makes and publishes a graph of the Stub app.
func (f *fixture) graph(id, kind string, nodes, edges []any) {
	f.t.Helper()
	g := m{"name": id, "kind": kind, "owner": "app:stub", "nodes": nodes, "edges": edges}
	res := f.ok("bp.import", id, m{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph."+id, res.Seq).Value, &d)
	res = f.ok("bp.publish", id, m{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph."+id, res.Seq)
}

func opNode(op string) map[string]any {
	return node("op", "connector.operation", m{"contract": connectorstest.ContractRef, "operation": op})
}

func expr(id, e string) map[string]any { return node(id, "data.expression", m{"expr": e}) }

func ret(outputs ...string) map[string]any {
	var list []any
	for _, o := range outputs {
		id, typ, _ := strings.Cut(o, ":")
		list = append(list, m{"id": id, "type": typ})
	}
	return node("ret", "flow.return", m{"outputs": list})
}

// stubGraphs makes the Stub app's operation graphs: echo as the builtin
// does it, fail as a typed error, start keyed (hitting once per run) and
// emitting finished, check, and an action that emits finished.
func (f *fixture) stubGraphs() {
	f.graph("op-stub-echo-echo", "connector.op",
		[]any{opNode("echo"),
			expr("text", `let n = args.repeat ?? 1; let s = repeat(args.text, int(n)); (args.options?.upper ?? false) ? upper(s) : s`),
			expr("length", `len(value)`), expr("by", `"scripted"`), ret("text:string", "length:number", "by:string")},
		[]any{edge("op", "then", "ret", "then"), edge("op", "args", "text", "args"), edge("text", "value", "length", "value"),
			edge("text", "value", "ret", "text"), edge("length", "value", "ret", "length"), edge("by", "value", "ret", "by")})
	f.graph("op-stub-echo-fail", "connector.op",
		[]any{opNode("fail"), expr("code", `args.code`), expr("message", `args.message ?? ""`), node("err", "connector.returnError", nil)},
		[]any{edge("op", "then", "err", "then"), edge("op", "args", "code", "args"), edge("op", "args", "message", "args"),
			edge("code", "value", "err", "code"), edge("message", "value", "err", "message")})
	f.graph("op-stub-echo-start", "connector.op",
		[]any{opNode("start"), expr("job", `"j-" + args.id`), expr("payload", `{id: "evt-" + args.id, jobId: "j-" + args.id}`),
			node("hit", "test.hit", nil), node("emit", "connector.emit", m{"contract": connectorstest.ContractRef, "event": "finished"}), ret("jobId:string")},
		[]any{edge("op", "then", "hit", "then"), edge("op", "args", "job", "args"), edge("job", "value", "hit", "value"),
			edge("hit", "then", "emit", "then"), edge("op", "connection", "emit", "connection"), edge("op", "args", "payload", "args"),
			edge("payload", "value", "emit", "payload"), edge("emit", "then", "ret", "then"), edge("job", "value", "ret", "jobId")})
	f.graph("op-stub-echo-check", "connector.op",
		[]any{opNode("check"), expr("ok", `settings.user != ""`), ret("ok:bool")},
		[]any{edge("op", "then", "ret", "then"), edge("op", "settings", "ok", "settings"), edge("ok", "value", "ret", "ok")})
	f.graph("stub-raise", livenodes.KindAction,
		[]any{node("call", cognition.EventCall, m{"outputs": []any{m{"id": "connection", "type": "string"}, m{"id": "id", "type": "string"}}}),
			expr("payload", `{id: id, jobId: "j-" + id}`), node("emit", "connector.emit", m{"contract": connectorstest.ContractRef, "event": "finished"}), ret("id:string")},
		[]any{edge("call", "then", "emit", "then"), edge("call", "connection", "emit", "connection"), edge("call", "id", "payload", "id"),
			edge("payload", "value", "emit", "payload"), edge("emit", "then", "ret", "then"), edge("emit", "id", "ret", "id")})
}

const stubSettings = `{"type":"object","properties":{"user":{"type":"string","title":"User"},"password":{"type":"secret","title":"Password"}},"required":["user"]}`

// stubConnector is the Stub app's connector.
func stubConnector(ops map[string]any) map[string]any {
	var settings any
	_ = json.Unmarshal([]byte(stubSettings), &settings)
	return m{"name": "echo", "title": "Stub echo", "description": "Implements test.echo@1 in blueprints. For tests only.",
		"contracts": []string{connectorstest.ContractRef}, "settings": settings, "operations": ops}
}

// publishStub makes the Stub app with its connector and publishes it.
func (f *fixture) publishStub() int {
	f.t.Helper()
	f.stubGraphs()
	f.ok("app.create", "", m{"name": "Stub"})
	f.ok("app.addConnector", "stub", stubConnector(m{"echo": "op-stub-echo-echo", "fail": "op-stub-echo-fail", "start": "op-stub-echo-start", "check": "op-stub-echo-check"}))
	return f.publish("stub")
}

func (f *fixture) publish(app string) int {
	f.t.Helper()
	var md manifests.Draft
	seq := f.ok("app.setInfo", app, m{"description": "For tests"}).Seq
	_ = json.Unmarshal(f.wait(manifests.DraftProjector, "app."+app, seq).Value, &md)
	res := f.ok("app.publish", app, m{"rev": md.Rev})
	n := md.Published + 1
	f.wait(manifests.VersionProjector, "appVersion."+apps.VersionID(app, n), res.Seq)
	return n
}

func (f *fixture) install(cmd, app string, a map[string]any) {
	f.t.Helper()
	res := f.ok(cmd, app, a)
	f.wait(installs.Projector, "appInstall."+app, res.Seq)
}

// serveScripted runs the scripted adapter, as every server process does.
func (f *fixture) serveScripted(name string) {
	nc, err := f.srv.Connect(name)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scripted.Serve(ctx, nc, func() scripted.Deps { return f.scripted }, func() ([]uint64, error) { return []uint64{features.LocalTenantID}, nil },
			scripted.Options{Every: 200 * time.Millisecond, Adapter: adapter.Options{Name: name, AnnounceEvery: 100 * time.Millisecond, TTL: 400 * time.Millisecond, LeaseEvery: 200 * time.Millisecond}})
	}()
	f.t.Cleanup(func() { cancel(); <-done; nc.Close() })
}

func (f *fixture) gone(typ string) {
	f.t.Helper()
	eventually(f.t, typ+" leaving the registry", 3*time.Second, func() bool {
		_, ok, _ := f.deps.Registry.Type(context.Background(), 1, typ)
		return !ok
	})
}

func TestScriptedTypeAnswersAsTheBuiltinDoes(t *testing.T) {
	f := setup(t)
	f.serve(connectorstest.Options("a", nil))
	f.serveScripted("scripted-1")
	f.serveScripted("scripted-2") // a second process: one queue group
	f.publishStub()
	f.install("app.install", "stub", m{})
	f.online("test.secure")
	f.online("stub.echo")
	ti, _, _ := f.deps.Registry.Type(context.Background(), 1, "stub.echo")
	if ti.Source != connectors.SourceScripted || ti.Instances != 2 {
		t.Fatalf("stub.echo is listed as %s with %d instances", ti.Source, ti.Instances)
	}
	builtin := f.create(m{"type": "test.secure", "name": "Builtin", "settings": m{"user": "ada"}, "secrets": m{"password": "pw"}})
	script := f.create(m{"type": "stub.echo", "name": "Scripted", "settings": m{"user": "ada"}, "secrets": m{"password": "pw"}})

	for _, a := range []string{`{"text":"hi"}`, `{"text":"ab","repeat":3}`, `{"text":"x","repeat":2,"options":{"upper":true}}`} {
		want, e1 := f.call(args(), builtin.ID, "echo", a)
		got, e2 := f.call(args(), script.ID, "echo", a)
		if e1 != nil || e2 != nil {
			t.Fatalf("echo %s: %v / %v", a, e1, e2)
		}
		if want["text"] != got["text"] || want["length"] != got["length"] || got["by"] != "scripted" {
			t.Fatalf("echo %s: builtin %v, scripted %v", a, want, got)
		}
	}
	for _, code := range []string{"notFound", "conflict", "rateLimited", "invalid"} {
		a := `{"code":"` + code + `","message":"as asked"}`
		_, e1 := f.call(args(), builtin.ID, "fail", a)
		_, e2 := f.call(args(), script.ID, "fail", a)
		if e1 == nil || e2 == nil || e1.Code != e2.Code || e1.Message != e2.Message || e1.Retryable() != e2.Retryable() {
			t.Fatalf("fail %s: builtin %+v, scripted %+v", code, e1, e2)
		}
	}
	// The ledger: a keyed operation runs its graph once per key
	for range 3 {
		out, e := f.call(args(), script.ID, "start", `{"id":"k1"}`)
		if e != nil || out["jobId"] != "j-k1" {
			t.Fatalf("start: %v %v", out, e)
		}
	}
	if n := len(slices.DeleteFunc(f.hits.all(), func(v any) bool { return v != "j-k1" })); n != 1 {
		t.Fatalf("start's graph ran %d times for one key", n)
	}
	// connection.test runs the check graph
	if r, err := connections.Test(args(), f.deps, script.ID); err != nil || !r.OK {
		t.Fatalf("connection.test: %+v %v", r, err)
	}
}

func TestScriptedOperationLeftOutIsNotSupported(t *testing.T) {
	f := setup(t)
	f.serveScripted("scripted")
	f.stubGraphs()
	f.ok("app.create", "", m{"name": "Stub"})
	res := f.ok("app.addConnector", "stub", stubConnector(m{"echo": "op-stub-echo-echo", "check": "op-stub-echo-check"}))
	f.wait(manifests.DraftProjector, "app.stub", res.Seq)
	errs, warnings, _ := manifests.CheckDraft(context.Background(), f.p, 1, f.reg, "stub")
	if len(errs) != 0 || !slices.ContainsFunc(warnings, func(p live.Problem) bool { return strings.HasSuffix(p.Path, "operations.fail") }) {
		t.Fatalf("errors %+v, warnings %+v", errs, warnings)
	}
	f.publish("stub")
	f.install("app.install", "stub", m{})
	f.online("stub.echo")
	c := f.create(m{"type": "stub.echo", "name": "Scripted", "settings": m{"user": "ada"}})
	if _, e := f.call(args(), c.ID, "fail", `{"code":"notFound"}`); e == nil || e.Code != connectors.CodeNotSupported || e.Retryable() {
		t.Fatalf("fail left out: %+v", e)
	}
}

func TestEmitStartsATriggerOncePerEventID(t *testing.T) {
	f := setup(t)
	f.runJobs()
	f.keep()
	f.serveScripted("scripted")
	f.publishStub()
	f.install("app.install", "stub", m{})
	f.installWatcher("")
	f.online("stub.echo")
	c := f.create(m{"type": "stub.echo", "name": "Scripted", "owner": "tenant", "settings": m{"user": "ada"}})
	raise := func(id string) {
		var g cognition.Graph
		_ = json.Unmarshal(f.wait(drafts.PublishedProjector, "graph.stub-raise", 0).Value, &g)
		g.ID = "stub-raise"
		as := features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(1, "stub")}
		if _, err := f.scripted.Engine.Run(engine.RunArgs{ServiceArgs: as, GraphID: g.ID, Graph: &g, Event: cognition.EventCall,
			Inputs: cognition.Values{"connection": c.ID, "id": id}}); err != nil {
			t.Fatalf("stub-raise: %v", err)
		}
	}
	// The trigger's consumer is made by the runner, a moment after install
	warm := 0
	eventually(t, "the trigger listening", 5*time.Second, func() bool {
		warm++
		raise(fmt.Sprintf("warm%d", warm))
		time.Sleep(100 * time.Millisecond)
		return slices.ContainsFunc(f.hits.all(), func(v any) bool { s, _ := v.(string); return strings.HasPrefix(s, "j-warm") })
	})
	raise("e1")
	raise("e1") // the same event again
	raise("e2")
	eventually(t, "two trigger runs", 5*time.Second, func() bool {
		return slices.Contains(f.hits.all(), "j-e1") && slices.Contains(f.hits.all(), "j-e2")
	})
	time.Sleep(500 * time.Millisecond)
	if n := len(slices.DeleteFunc(f.hits.all(), func(v any) bool { return v != "j-e1" })); n != 1 {
		t.Fatalf("e1 started the trigger %d times", n)
	}
	// Not on a connection of another type
	other := f.create(m{"type": "test.secure", "name": "Other", "settings": m{"user": "ada"}, "secrets": m{"password": "pw"}})
	var g cognition.Graph
	_ = json.Unmarshal(f.wait(drafts.PublishedProjector, "graph.stub-raise", 0).Value, &g)
	as := features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(1, "stub")}
	if _, err := f.scripted.Engine.Run(engine.RunArgs{ServiceArgs: as, GraphID: "stub-raise", Graph: &g, Event: cognition.EventCall,
		Inputs: cognition.Values{"connection": other.ID, "id": "e3"}}); err == nil || !strings.Contains(err.Error(), "not one of the app stub's") {
		t.Fatalf("emit on a test.echo connection: %v", err)
	}
}

func TestScriptedTypeComesAndGoesWithItsInstall(t *testing.T) {
	f := setup(t)
	f.keep()
	f.serveScripted("scripted")
	changes, stop, err := f.deps.Registry.Changes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	drain := func() {
		for {
			select {
			case <-changes:
			default:
				return
			}
		}
	}
	saw := func(what string) {
		t.Helper()
		select {
		case <-changes:
		case <-time.After(3 * time.Second):
			t.Fatalf("the registry's subscribers saw no change on %s", what)
		}
	}
	if _, ok, _ := f.deps.Registry.Type(context.Background(), 1, "stub.echo"); ok {
		t.Fatal("stub.echo is listed before its app is installed")
	}
	f.publishStub()
	drain()
	f.install("app.install", "stub", m{})
	saw("install")
	f.online("stub.echo")
	c := f.create(m{"type": "stub.echo", "name": "Scripted", "settings": m{"user": "ada"}})
	drain()
	f.install("app.uninstall", "stub", m{})
	saw("uninstall")
	f.gone("stub.echo")
	eventually(t, "the connection disabled", 3*time.Second, func() bool {
		got, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
		return got.Status == connections.StatusDisabled
	})
	if _, e := f.call(args(), c.ID, "echo", `{"text":"hi"}`); e == nil {
		t.Fatal("a call on the uninstalled type's connection was answered")
	}
}

func TestManifestOperationWithoutItsGraphIsRefused(t *testing.T) {
	f := setup(t)
	f.stubGraphs()
	f.ok("app.create", "", m{"name": "Stub"})
	for _, tc := range []struct {
		ops  map[string]any
		path string
	}{
		{m{"echo": "nope"}, "connectors[echo].operations.echo"},                    // no such graph
		{m{"echo": "op-stub-echo-fail"}, "connectors[echo].operations.echo"},       // answers fail
		{m{"echo": "stub-raise"}, "connectors[echo].operations.echo"},              // not connector.op
		{m{"publish": "op-stub-echo-echo"}, "connectors[echo].operations.publish"}, // no such operation
	} {
		res := f.doAs(args(), "app.addConnector", "stub", stubConnector(tc.ops))
		if res.Common.Code < 300 || len(res.Problems) == 0 {
			t.Fatalf("%v was accepted", tc.ops)
		}
		p := res.Problems[0]
		if !strings.HasSuffix(p.Path, tc.path) || p.Fix == "" {
			t.Fatalf("%v: %+v", tc.ops, res.Problems)
		}
	}
	// A Return missing a required result field
	f.graph("op-stub-echo-bad", "connector.op", []any{opNode("echo"), expr("by", `"x"`), ret("by:string")},
		[]any{edge("op", "then", "ret", "then"), edge("by", "value", "ret", "by")})
	res := f.doAs(args(), "app.addConnector", "stub", stubConnector(m{"echo": "op-stub-echo-bad"}))
	if res.Common.Code < 300 || !strings.Contains(res.Problems[0].Message, "returns no text") || res.Problems[0].Fix == "" {
		t.Fatalf("a Return without text: %+v", res.Problems)
	}
}
