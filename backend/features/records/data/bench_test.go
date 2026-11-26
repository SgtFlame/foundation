package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
)

// BenchmarkQueries reads 100 000 records by index and by scan. The records
// are published to the stream as created events, not sent as commands, so
// loading them measures the projector (and its index entries) alone. Run
// it with go test -run '^$' -bench Queries -benchtime 20x.
func BenchmarkQueries(b *testing.B) {
	const n = 100_000
	f := setup(b)
	f.define("item", [][]string{{"n"}, {"done", "due"}})
	ctx := context.Background()
	if _, err := f.p.Stream(ctx, features.LocalTenantID, live.AreaRecords); err != nil {
		b.Fatal(err)
	}
	if _, err := f.p.Bucket(ctx, features.LocalTenantID, data.Projector); err != nil {
		b.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 1))
	start := time.Now()
	for i := range n {
		id := fmt.Sprint(1_000_000 + i)
		values := map[string]any{
			"title": fmt.Sprintf("item %d", r.IntN(n)), "n": r.IntN(1000), "done": r.IntN(4) == 0,
			"due": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, r.IntN(730)).Format(time.DateOnly),
		}
		raw, _ := json.Marshal(map[string]any{"values": values})
		t := live.Target{Area: live.AreaRecords, Type: "item", ID: id}
		e, _ := json.Marshal(live.Event{Type: data.EventCreated, Target: t, Data: raw, CommandID: "bench-" + id, TenantID: features.LocalTenantID, Time: time.Now()})
		if _, err := f.js.PublishAsync(t.Subject(features.LocalTenantID, data.EventCreated), e); err != nil {
			b.Fatal(err)
		}
		if i%4096 == 4095 {
			<-f.js.PublishAsyncComplete()
		}
	}
	<-f.js.PublishAsyncComplete()
	published := time.Since(start)
	settle, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := f.p.Settle(settle, features.LocalTenantID, data.Projector); err != nil {
		b.Fatal(err)
	}
	b.Logf("%d records published in %v, projected with their index entries in %v", n, published.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))

	d := data.Deps{Projections: f.p, Settings: func(features.ServiceArgs) recordssettings.Records { return recordssettings.Records{ScanLimit: 2 * n} }}
	start = time.Now()
	if _, err := data.Find(args(), d, data.Query{Entity: "item", Sort: []data.Sort{{Field: "n"}}, Limit: 1}); err != nil {
		b.Fatal(err)
	}
	if _, err := data.Find(args(), d, data.Query{Entity: "item", Where: map[string]json.RawMessage{"done": json.RawMessage(`true`)}, Sort: []data.Sort{{Field: "due"}}, Limit: 1}); err != nil {
		b.Fatal(err)
	}
	b.Logf("both indexes loaded into memory in %v", time.Since(start).Round(time.Millisecond))

	for _, c := range []struct {
		name string
		q    data.Query
	}{
		{"indexed/eq-sorted-page", data.Query{Where: map[string]json.RawMessage{"done": json.RawMessage(`false`)}, Sort: []data.Sort{{Field: "due"}}, Limit: 50}},
		{"indexed/sort-page", data.Query{Sort: []data.Sort{{Field: "n", Desc: true}}, Limit: 50}},
		{"indexed/eq-100", data.Query{Where: map[string]json.RawMessage{"n": json.RawMessage(`500`)}}},
		{"indexed/range-1000", data.Query{Filter: &data.Filter{And: []data.Filter{{Field: "n", Op: "gte", Value: json.RawMessage(`100`)}, {Field: "n", Op: "lt", Value: json.RawMessage(`110`)}}}, Limit: 50}},
		{"scan/filter-page", data.Query{Filter: &data.Filter{Field: "title", Op: "prefix", Value: json.RawMessage(`"item 9999"`)}, Limit: 50}},
		{"scan/sort-page", data.Query{Sort: []data.Sort{{Field: "title"}}, Limit: 50}},
	} {
		c.q.Entity = "item"
		b.Run(c.name, func(b *testing.B) {
			var res data.Result
			var err error
			for b.Loop() {
				if res, err = data.Find(args(), d, c.q); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(res.Plan.Read), "read/op")
			b.ReportMetric(float64(len(res.Records)), "records/op")
		})
	}
}
