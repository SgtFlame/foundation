package impl

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice/server"
)

// viaNATS serves real on a test NATS server and returns its proxy.
func viaNATS(t *testing.T, real usageservice.Service) usageservice.Service {
	t.Helper()
	ns := natsd.StartTest(t)
	srv, err := server.Serve(ns.Conn(), real, services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return proxy.New(&services.Client{NC: ns.Conn(), Caller: "test"})
}

func TestProxyCaptureAndRecords(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, usage.Models()...); err != nil {
		t.Fatal(err)
	}
	rec := usage.Record{Tenanted: db.Tenanted{TenantID: 1}, Feature: "chat", ModelName: "m", CostMicros: 5}
	if err := gdb.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	p := viaNATS(t, &service{db: gdb})
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}
	r := p.Records(usageservice.RecordsArgs{ServiceArgs: args})
	if r.Common.Code != http.StatusOK || r.Total != 1 || len(r.Records) != 1 || r.Records[0].ID != rec.ID || r.Records[0].ModelName != "m" {
		t.Fatalf("records %+v", r)
	}
	// A failure keeps its status, message and error code across the hop.
	c := p.Capture(usageservice.CaptureArgs{ServiceArgs: args, RecordID: 42})
	want := (&service{db: gdb}).Capture(usageservice.CaptureArgs{ServiceArgs: args, RecordID: 42}).Common
	if c.Common.Code != want.Code || c.Common.Message != want.Message || c.Common.ErrorCode != want.ErrorCode || c.Common.Code != http.StatusNotFound {
		t.Fatalf("capture %+v, want %+v", c.Common, want)
	}
}

// A recorder in another process appends through the proxy; a retried
// batch stores each record once (forge 21d-2).
func TestRecorderAppendsThroughTheProxy(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, usage.Models()...); err != nil {
		t.Fatal(err)
	}
	store := usageservice.Store(viaNATS(t, &service{db: gdb}))
	recs := []usage.Record{{Tenanted: db.Tenanted{TenantID: 1}, Feature: "chat", ModelName: "m"}}
	recs[0].ID = features.NewTSID()
	caps := []usage.Capture{{Tenanted: db.Tenanted{TenantID: 1}, RecordID: recs[0].ID, System: "s"}}
	for i := 0; i < 2; i++ {
		if err := store.Append(t.Context(), recs, caps); err != nil {
			t.Fatal(err)
		}
	}
	var n, c int64
	gdb.Model(&usage.Record{}).Count(&n)
	gdb.Model(&usage.Capture{}).Count(&c)
	if n != 1 || c != 1 {
		t.Fatalf("%d records, %d captures", n, c)
	}
	// A person cannot append.
	r := (&service{db: gdb}).Append(usageservice.AppendArgs{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}, Records: recs})
	if r.Common.Code != http.StatusForbidden {
		t.Fatalf("a person appended: %+v", r.Common)
	}
}
