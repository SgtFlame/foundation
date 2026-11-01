package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// Converter is the durable consumer that converts records' values when a
// published version changes a field's type.
const Converter = "records.converter"

// Follow makes sure the principal's tenant converts its records when a
// field's type changes. It runs as a follower of the def stream's
// published events: entity.publish starts it before its event commits
// (entities.Deps.OnPublish), and the record commands and records.list start
// it after a restart, catching up on what was published meanwhile.
func Follow(args features.ServiceArgs, deps Deps) error {
	if deps.Bus == nil || deps.Projections == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	subject := live.Target{Area: live.AreaDefinitions, Type: entities.TargetType, ID: "*"}.Subject(tenant, entities.EventPublished)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, Converter, []string{subject}, func(e live.Event) error {
		return convert(deps, e)
	})
}

// convert sends record.convert for every record holding a value of a
// field whose type changed. Each command's id names the event, record and
// field, and a value that already fits is left alone, so a redelivered
// event changes nothing.
func convert(deps Deps, e live.Event) error {
	var p entities.Publication
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return fmt.Errorf("cannot read the publication: %w", err)
	}
	if len(p.TypeChanges) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries, err := deps.Projections.Watch(ctx, e.TenantID, Projector, e.Target.ID+".*")
	if err != nil {
		return err
	}
	var todo []Record
	for en := range entries {
		if en == nil {
			break
		}
		var r Record
		if !en.Deleted && json.Unmarshal(en.Value.Value, &r) == nil {
			todo = append(todo, r)
		}
	}
	cancel()
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.ServicePrincipal(e.TenantID)}
	for _, r := range todo {
		var id string
		_ = json.Unmarshal(r[MetaID], &id)
		for _, c := range p.TypeChanges {
			v := r[c.Field]
			if records.IsNull(v) {
				continue
			}
			if _, err := records.CheckValue(c.To, v); err == nil {
				continue
			}
			a, _ := json.Marshal(map[string]any{"entity": e.Target.ID, "field": c.Field, "from": c.From, "to": c.To})
			res := deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("convert-%d-%s-%s", e.Seq, id, c.Field), Type: "record.convert", Target: id, Args: a})
			if res.Common.Code >= 300 && res.Common.Code != 422 {
				return fmt.Errorf("cannot convert %s of %s: %s", c.Field, id, res.Common.Message)
			}
		}
	}
	return nil
}
