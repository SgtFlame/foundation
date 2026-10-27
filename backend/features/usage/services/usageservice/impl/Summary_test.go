package impl

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

func TestSummaryBucketsAndGroups(t *testing.T) {
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, usage.Models()...); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 22, 23, 30, 0, 0, time.UTC)
	records := []usage.Record{
		{Feature: "chat", Level: "standard", ConversationID: 7, InputTokens: 100, OutputTokens: 10, CostMicros: 300},
		{Feature: "chat", Level: "deep", ConversationID: 8, InputTokens: 50, OutputTokens: 5, CostMicros: 900},
		{Feature: "embed-index", EmbedTokens: 1000, CostMicros: 150},
	}
	for i := range records {
		records[i].TenantID = 1
		records[i].CreatedAt = day.Add(time.Duration(i) * time.Hour) // 23:30 on the 22nd, then 00:30 and 01:30 on the 23rd
	}
	records = append(records, usage.Record{Tenanted: db.Tenanted{TenantID: 2}, Feature: "chat", CostMicros: 99999})
	if err := gdb.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	s := &service{db: gdb}
	args := features.ServiceArgs{Principal: features.LocalPrincipal()}

	resp := s.Summary(usageservice.SummaryArgs{ServiceArgs: args, Bucket: "day", GroupBy: "feature"})
	if resp.Common.Code != http.StatusOK || resp.Totals.Requests != 3 || resp.Totals.CostMicros != 1350 || resp.Totals.EmbedTokens != 1000 {
		t.Fatalf("totals %+v", resp)
	}
	want := []usageservice.SummaryRow{
		{Bucket: "2026-10-22", Group: "chat"}, {Bucket: "2026-10-23", Group: "chat"}, {Bucket: "2026-10-23", Group: "embed-index"},
	}
	if len(resp.Rows) != len(want) {
		t.Fatalf("rows %+v", resp.Rows)
	}
	for i, w := range want {
		if resp.Rows[i].Bucket != w.Bucket || resp.Rows[i].Group != w.Group {
			t.Errorf("row %d = %+v, want %+v", i, resp.Rows[i], w)
		}
	}
	// At UTC+1 the first record falls on the 23rd too
	resp = s.Summary(usageservice.SummaryArgs{ServiceArgs: args, Bucket: "day", TZOffsetMinutes: 60})
	if len(resp.Rows) != 1 || resp.Rows[0].Bucket != "2026-10-23" || resp.Rows[0].Requests != 3 {
		t.Errorf("UTC+1 rows %+v", resp.Rows)
	}
	// The costliest conversation, without buckets
	resp = s.Summary(usageservice.SummaryArgs{ServiceArgs: args, GroupBy: "conversation", Feature: "chat", Limit: 1})
	if len(resp.Rows) != 1 || resp.Rows[0].Group != "8" || resp.Rows[0].CostMicros != 900 {
		t.Errorf("top conversation %+v", resp.Rows)
	}
	resp = s.Summary(usageservice.SummaryArgs{ServiceArgs: args, From: day.Add(30 * time.Minute)})
	if resp.Totals.Requests != 2 {
		t.Errorf("from 00:00 totals %+v", resp.Totals)
	}
	if code := s.Summary(usageservice.SummaryArgs{ServiceArgs: args, GroupBy: "user; DROP"}).Common.Code; code != http.StatusBadRequest {
		t.Errorf("bad group: code %d", code)
	}
}
