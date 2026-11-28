package impl

import (
	"context"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
	filesimpl "github.com/SgtFlame/foundation/backend/features/records/services/filesservice/impl"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice/server"
)

const contacts = "Name;Email;Deals\nAda;ada@example.com;3\nBob;bob@example.com;1\nCyd;cyd@example.com;0\n"

func as(user uint64) features.ServiceArgs {
	p := features.LocalPrincipal()
	p.UserID = user
	return features.ServiceArgs{Ctx: context.Background(), Principal: p}
}

// The CSV a user uploaded parses in process and over NATS alike; another
// user's file is not there for them (forge 40c).
func TestParseCSV(t *testing.T) {
	ns := natsd.StartTest(t)
	store := files.New(ns.JS())
	fs := filesimpl.New(filesimpl.WithStore(store))
	tenant := features.LocalPrincipal().TenantID
	ref, err := store.Put(context.Background(), tenant, 1, "contacts.csv", "text/csv", strings.NewReader(contacts), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	real := New(WithFiles(func() filesservice.Service { return fs }))
	srv, err := server.Serve(ns.Conn(), real, services.TrustHeaders())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	remote := proxy.New(&services.Client{NC: ns.Conn(), Caller: "test"})

	for name, svc := range map[string]parseservice.Service{"in process": real, "over NATS": remote} {
		t.Run(name, func(t *testing.T) {
			res := svc.ParseCSV(parseservice.ParseCSVArgs{ServiceArgs: as(1), File: ref.ID, Sample: 2})
			if res.Common.Code != 200 {
				t.Fatalf("parse: %+v", res.Common)
			}
			if res.Name != "contacts.csv" || res.Delimiter != ";" || !res.Header || res.Total != 3 || len(res.Rows) != 2 {
				t.Fatalf("parse: %+v", res)
			}
			if res.Columns[1].Type != "email" || res.Columns[2].Type != "number" || res.Rows[0]["Name"] != "Ada" {
				t.Fatalf("columns %+v rows %+v", res.Columns, res.Rows)
			}
			if d, ok := res.Rows[0]["Deals"].(float64); ok && d != 3 || !ok && res.Rows[0]["Deals"] != int64(3) {
				t.Fatalf("deals %#v", res.Rows[0]["Deals"])
			}
			if res := svc.ParseCSV(parseservice.ParseCSVArgs{ServiceArgs: as(2), File: ref.ID}); res.Common.Code != 404 || res.Common.ErrorCode != "file_not_found" {
				t.Fatalf("another user's file: %+v", res.Common)
			}
			if res := svc.ParseCSV(parseservice.ParseCSVArgs{ServiceArgs: as(1), Text: "n\n1\n2\n3\n", MaxRows: 2}); res.Common.Code != 413 || res.Common.ErrorCode != "too_many_rows" {
				t.Fatalf("too many rows: %+v", res.Common)
			}
			if res := svc.ParseCSV(parseservice.ParseCSVArgs{ServiceArgs: as(1), Text: "a,b\n1,2\n"}); res.Common.Code != 200 || res.Rows[0]["b"] == nil {
				t.Fatalf("inline: %+v", res)
			}
		})
	}
}
