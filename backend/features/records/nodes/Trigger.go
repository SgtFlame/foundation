package recordsnodes

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// OnChanged is On record changed, the records' trigger event (forge 07a).
const OnChanged = "records.onChanged"

type onChangedConfig struct {
	Entity string   `json:"entity"`
	Events []string `json:"events,omitempty"`
}

// changeEvents are the record events each change On record changed names
// starts on: a list-like field's items added or removed are an update.
// Conversions and recomputed values are the system's, not changes.
var changeEvents = map[string][]string{
	"created": {data.EventCreated},
	"updated": {data.EventUpdated, data.EventAdded, data.EventRemoved},
	"deleted": {data.EventDeleted},
}

// changeOf is the change a record event is.
func changeOf(event string) string {
	for change, events := range changeEvents {
		if slices.Contains(events, event) {
			return change
		}
	}
	return event
}

// contributeTrigger registers On record changed as a trigger event: a
// trigger starts on each create, update or delete of the entity type's
// records, and only on a type its app may read.
func contributeTrigger(reg *extensions.Registry, deps func() data.Deps) {
	livenodes.TriggerEvents.Register(reg, livenodes.TriggerEvent{
		Descriptor: extensions.Descriptor{ID: OnChanged, Source: source},
		Subjects: func(args features.ServiceArgs, _ string, config json.RawMessage) (live.Area, []string, error) {
			var c onChangedConfig
			if err := json.Unmarshal(config, &c); err != nil {
				return "", nil, fmt.Errorf("On record changed's config cannot be read: %v", err)
			}
			if !live.ValidToken(c.Entity) || c.Entity[0] == '_' {
				return "", nil, fmt.Errorf("On record changed needs an entity type, not %q", c.Entity)
			}
			resource := records.Resource(args.Principal, c.Entity)
			if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
				return "", nil, fmt.Errorf("the app may not read the records of %s, so a trigger cannot start on them", c.Entity)
			}
			changes := c.Events
			if len(changes) == 0 {
				changes = []string{"created", "updated", "deleted"}
			}
			var events []string
			for _, ch := range changes {
				evs, ok := changeEvents[ch]
				if !ok {
					return "", nil, fmt.Errorf("%q is not a change: created, updated or deleted", ch)
				}
				events = append(events, evs...)
			}
			return live.AreaRecords, livenodes.EventSubjects(args.Principal.TenantID, live.AreaRecords, c.Entity, events), nil
		},
		// The record as the job's principal reads it when the job runs:
		// nothing once deleted or when its read rule hides it
		Inputs: func(args features.ServiceArgs, config json.RawMessage, e live.Event) (cognition.Values, error) {
			var c onChangedConfig
			_ = json.Unmarshal(config, &c)
			var change struct {
				Old map[string]any `json:"old"`
			}
			_ = json.Unmarshal(e.Data, &change)
			out := cognition.Values{"event": changeOf(e.Type), "old": change.Old, "record": nil, "id": e.Target.ID}
			if e.Type == data.EventDeleted {
				return out, nil
			}
			r, _, err := data.Get(args, deps(), e.Target.Type, e.Target.ID)
			if err != nil {
				return nil, err
			}
			if r != nil {
				raw, _ := json.Marshal(r)
				var rec map[string]any
				_ = json.Unmarshal(raw, &rec)
				out["record"] = rec
			}
			return out, nil
		},
	})
}
