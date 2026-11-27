package impl

import (
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice/server"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
)

// An engine in another process keeps its traces through the proxy, a
// retried save keeps one copy, and a third reads them back (forge 21d-2).
func TestTracesThroughTheProxy(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, cognition.Models()...); err != nil {
		t.Fatal(err)
	}
	ns := natsd.StartTest(t)
	srv, err := server.Serve(ns.Conn(), &service{db: gdb}, services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	traces := cognitionservice.Traces(proxy.New(&services.Client{NC: ns.Conn(), Caller: "runner"}))

	run := cognition.Run{Tenanted: db.Tenanted{TenantID: 1}, GraphID: "g", Event: "e", Status: "done"}
	run.ID = features.NewTSID()
	steps := []cognition.Step{{RunID: run.ID, Seq: 1, NodeID: "a"}, {RunID: run.ID, Seq: 2, NodeID: "b"}}
	if err := traces.Begin(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := traces.Save(t.Context(), run, steps); err != nil {
			t.Fatal(err)
		}
	}
	got, gotSteps, found, err := traces.Get(t.Context(), 1, run.ID)
	if err != nil || !found || got.GraphID != "g" || len(gotSteps) != 2 || gotSteps[1].NodeID != "b" {
		t.Fatalf("read back %+v %+v %v %v", got, gotSteps, found, err)
	}
	if _, _, found, _ := traces.Get(t.Context(), 4, run.ID); found {
		t.Fatal("another tenant read the trace")
	}
	if err := traces.Purge(t.Context(), 10, 1); err != nil {
		t.Fatal(err)
	}
}
