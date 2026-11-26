package connections_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// installHolder installs the app holder: an entity type of its own,
// holderAccount, with a connection field, and an unfiltered trigger on the
// test contract's finished events.
func (f *fixture) installHolder() {
	f.t.Helper()
	res := f.ok("entity.create", "holderAccount", map[string]any{"name": "Holder account", "owner": "app:holder", "fields": []map[string]any{
		{"name": "name", "type": "text"}, {"name": "conn", "type": "connection"}}})
	var ed struct{ Rev int }
	_ = json.Unmarshal(f.wait(entities.DraftProjector, "entity.holderAccount", res.Seq).Value, &ed)
	res = f.ok("entity.publish", "holderAccount", map[string]any{"rev": ed.Rev})
	f.wait(entities.PublishedProjector, "entity.holderAccount", res.Seq)
	g := map[string]any{"name": "On finished", "kind": livenodes.KindTrigger, "owner": "app:holder",
		"nodes": []any{node("event", connectorsnodes.On, map[string]any{"contract": connectorstest.ContractRef, "event": "finished"}), node("hit", "test.hit", nil)},
		"edges": []any{edge("event", "then", "hit", "then"), edge("event", "connection", "hit", "value")},
	}
	res = f.ok("bp.import", "holder-finished", map[string]any{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph.holder-finished", res.Seq).Value, &d)
	res = f.ok("bp.publish", "holder-finished", map[string]any{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph.holder-finished", res.Seq)
	f.ok("app.create", "", map[string]any{"name": "Holder"})
	f.ok("app.addEntity", "holder", map[string]any{"entity": "holderAccount"})
	f.ok("app.addTrigger", "holder", map[string]any{"id": "finished", "graph": "holder-finished"})
	var md manifests.Draft
	seq := f.ok("app.setInfo", "holder", map[string]any{"description": "Holds accounts"}).Seq
	_ = json.Unmarshal(f.wait(manifests.DraftProjector, "app.holder", seq).Value, &md)
	res = f.ok("app.publish", "holder", map[string]any{"rev": md.Rev})
	f.wait(manifests.VersionProjector, "appVersion."+apps.VersionID("holder", 1), res.Seq)
	res = f.ok("app.install", "holder", map[string]any{})
	f.wait(installs.Projector, "appInstall.holder", res.Seq)
}

// A connection a record of the app's own type holds may be used by the
// app's principal (forge 07n question 4), a user's own included, until no
// record holds it; only someone who may use a connection sets it on a
// record; and the event source starts and stops with the record.
func TestAppUsesTheConnectionsItsRecordsHold(t *testing.T) {
	f := setup(t)
	f.keep()
	f.runJobs()
	f.serve(connectorstest.Options("s", nil))
	f.online("test.secure")
	f.installHolder()
	mine := f.create(map[string]any{"type": "test.secure", "name": "Mine", "settings": map[string]any{"user": "u", "events": 2},
		"secrets": map[string]any{"password": "p"}, "options": map[string]any{"pollInterval": "1s"}})
	theirs := f.connection(func() (string, uint64) {
		res := f.doAs(user(2), "connection.create", "", map[string]any{"type": "test.secure", "name": "Theirs", "settings": map[string]any{"user": "u"}, "secrets": map[string]any{"password": "p"}})
		return res.Target, res.Seq
	}())
	app := features.ServiceArgs{Ctx: context.Background(), Principal: features.AppPrincipal(1, "holder")}
	if err := f.deps.CheckUse(app, mine); err == nil {
		t.Fatalf("the app used a user's own connection no record holds")
	}
	if _, ok := f.source(mine.ID); ok {
		t.Fatalf("a source for a connection the app may not use")
	}
	// Another user may not lend Mine to the app
	res := f.doAs(user(2), "record.create", "", map[string]any{"entity": "holderAccount", "values": map[string]any{"name": "Stolen", "conn": mine.ID}})
	if res.Common.Code < 300 {
		t.Fatalf("user 2 set user 1's connection on a record")
	}
	// Its owner may
	res = f.ok("record.create", "", map[string]any{"entity": "holderAccount", "values": map[string]any{"name": "Mine", "conn": mine.ID}})
	held := res.Target
	f.wait(data.Projector, "holderAccount."+held, res.Seq)
	if err := f.deps.CheckUse(app, mine); err != nil {
		t.Fatalf("the app could not use the connection its record holds: %v", err)
	}
	if err := f.deps.CheckUse(app, theirs); err == nil {
		t.Errorf("the app used a connection no record holds")
	}
	if _, e := f.call(app, mine.ID, "echo", `{"text":"x"}`); e != nil {
		t.Errorf("the app's call on the held connection: %v", e)
	}
	if _, e := f.call(app, theirs.ID, "echo", `{"text":"x"}`); e == nil {
		t.Errorf("the app called a connection no record holds")
	}
	te, _ := livenodes.TriggerEventOf(f.reg, connectorsnodes.On)
	cfg, _ := json.Marshal(map[string]any{"contract": connectorstest.ContractRef, "event": "finished"})
	if !te.Accept(app, cfg, live.Event{Target: live.Target{ID: mine.ID}}) || te.Accept(app, cfg, live.Event{Target: live.Target{ID: theirs.ID}}) {
		t.Errorf("the trigger's accept does not follow the held connections")
	}
	// The source starts with the record, without waiting for the minute's pass
	eventually(t, "the held connection's source", 5*time.Second, func() bool { _, ok := f.source(mine.ID); return ok })
	eventually(t, "its events reach the trigger", 6*time.Second, func() bool { return len(f.hits.all()) >= 2 })
	// Cleared, it is the app's no more
	res = f.ok("record.update", held, map[string]any{"entity": "holderAccount", "set": map[string]any{"conn": nil}})
	f.wait(data.Projector, "holderAccount."+held, res.Seq)
	if err := f.deps.CheckUse(app, mine); err == nil {
		t.Errorf("the app still used the connection once the record let it go")
	}
	eventually(t, "the source gone", 5*time.Second, func() bool { _, ok := f.source(mine.ID); return !ok })
	if h, err := connections.Held(context.Background(), f.deps, 1, "holder"); err != nil || len(h) != 0 {
		t.Errorf("held after the record let go: %v %v", h, err)
	}
}
