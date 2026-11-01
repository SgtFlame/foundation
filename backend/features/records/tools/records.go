package recordstools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ContributeRecords registers the record tools: a one-time read and an
// import. Every record.* command is already a tool of its own (live/tools);
// svc returns the record service once made.
func ContributeRecords(reg *extensions.Registry, svc func() recordservice.Service) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "records.query", Source: "records"}, Title: "Read records", Effect: tools.EffectRead,
		Description: "Reads a page of an entity type's records once: each record's values by field id, and _id, _seq (for record.update's expect) and the rest of its own data under '_'. where keeps those whose fields equal the values given; filter is {field, op, value} (op " + strings.Join(data.Ops, ", ") + ") or {and: [...]} / {or: [...]} of them; sort is [{field, desc}] (record id order without one). next is the cursor of the following page. plan says whether an index answered it or every record was read (scan), and complete is false when a scan stopped at the scan limit. Change records with the record.* tools.",
		Input:       json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","description":"The entity type's id"},"where":{"type":"object","description":"Field id → value the records must have"},"filter":{"type":"object","description":"{field, op, value}, or {and: [filters]} or {or: [filters]}"},"sort":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"desc":{"type":"boolean"}},"required":["field"]}},"limit":{"type":"integer","description":"At most this many (50 when left out)"},"cursor":{"type":"string","description":"A previous result's next, for the following page"},"fields":{"type":"array","items":{"type":"string"},"description":"The field ids to return; all when left out"}},"required":["entity"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			q, err := tools.Decode[data.Query](call)
			if err != nil {
				return tools.Result{}, err
			}
			if q.Limit <= 0 {
				q.Limit = 50
			}
			resp := svc().Find(recordservice.FindArgs{ServiceArgs: call.ServiceArgs, Query: q})
			if resp.Common.Code >= 300 {
				return tools.Result{}, fmt.Errorf("%s", resp.Common.Message)
			}
			how := "by a scan"
			if resp.Plan.Index != "" {
				how = "from index " + resp.Plan.Index
			}
			more := ""
			if resp.Next != "" {
				more = "; more follow"
			}
			if !resp.Plan.Complete {
				more += "; the scan stopped at its limit, so some records were not read"
			}
			return tools.Result{Content: map[string]any{"records": resp.Records, "next": resp.Next, "plan": resp.Plan},
				Summary: fmt.Sprintf("%d %s records, %s%s", len(resp.Records), q.Entity, how, more)}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "records.import", Source: "records"}, Title: "Import records", Effect: tools.EffectWrite,
		Description: "Creates records of an entity type from rows of values by field id, such as sample data: one record.create per row. Reports how many were created, their ids, and the problems of rows that failed.",
		Input:       json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","description":"The entity type's id"},"rows":{"type":"array","items":{"type":"object"},"description":"Each row's values: field id → value"}},"required":["entity","rows"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Entity string
				Rows   []map[string]json.RawMessage
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := svc().Import(recordservice.ImportArgs{ServiceArgs: call.ServiceArgs, Entity: a.Entity, Rows: a.Rows})
			if resp.Common.Code >= 300 {
				return tools.Result{}, fmt.Errorf("%s", resp.Common.Message)
			}
			return tools.Result{Content: map[string]any{"created": resp.Created, "ids": resp.IDs, "failed": resp.Failed},
				Summary: fmt.Sprintf("Imported %d of %d %s records", resp.Created, len(a.Rows), a.Entity)}, nil
		},
	})
}
