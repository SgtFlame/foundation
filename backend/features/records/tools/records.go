package recordstools

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ContributeRecords registers the record tools: a one-time read and an
// import. Every record.* command is already a tool of its own (live/tools);
// svc returns the record service once made.
func ContributeRecords(reg *extensions.Registry, svc func() recordservice.Service) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "records.query", Source: "records"}, Title: "Read records", Effect: tools.EffectRead,
		Description: "Reads an entity type's records once, oldest first: each record's values by field id, and _id, _seq (for record.update's expect) and the rest of its own data under '_'. where keeps those whose fields equal the values given. Change records with the record.* tools.",
		Input:       json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","description":"The entity type's id"},"where":{"type":"object","description":"Field id → value the records must have"},"limit":{"type":"integer","description":"At most this many (50 when left out)"}},"required":["entity"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Entity string
				Where  map[string]json.RawMessage
				Limit  int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := svc().List(recordservice.ListArgs{ServiceArgs: call.ServiceArgs, Entity: a.Entity, Where: a.Where})
			if resp.Common.Code >= 300 {
				return tools.Result{}, fmt.Errorf("%s", resp.Common.Message)
			}
			limit := a.Limit
			if limit <= 0 {
				limit = 50
			}
			shown := resp.Records[:min(limit, len(resp.Records))]
			return tools.Result{Content: map[string]any{"records": shown, "total": len(resp.Records)},
				Summary: fmt.Sprintf("%d of %d %s records", len(shown), len(resp.Records), a.Entity)}, nil
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
