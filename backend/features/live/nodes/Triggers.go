package livenodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/nats-io/nats.go/jetstream"
)

// Trigger event node types (forge 07a).
const (
	OnEvent    = "live.onEvent"
	OnSchedule = "live.onSchedule"
	OnWebhook  = "live.onWebhook"
)

// TriggerEvent is how an app.trigger graph's event node is started by
// committed events (forge 07a): the subjects of the tenant's event stream
// it starts on, and its outputs for one event. The jobs feature keeps a
// durable consumer of those subjects per installed trigger, and enqueues a
// job for each event. On schedule is the jobs feature's own.
type TriggerEvent struct {
	extensions.Descriptor // ID: the event node's type
	// Subjects are the stream (by its area) and the subjects in it that
	// start a trigger of app with this config. An error refuses the
	// trigger, which then does not run. args are the app principal's: a
	// trigger may start only on events its app may read.
	Subjects func(args features.ServiceArgs, app string, config json.RawMessage) (live.Area, []string, error)
	// Inputs are the event node's outputs for one event, read as the job's
	// principal when the job runs.
	Inputs func(args features.ServiceArgs, config json.RawMessage, e live.Event) (cognition.Values, error)
	// Stream, when set, opens the stream the subjects are in, for events
	// that are not committed events (a connector's, in CX_<tenant>, forge
	// 07c); Subjects' area then only names it in the consumer's key. Its
	// messages are live.Events all the same.
	Stream func(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.Stream, error)
	// Accept, when set, decides as the app's principal whether one event
	// starts the trigger; a refused event is skipped, with no job (a
	// connection the app may not use).
	Accept func(args features.ServiceArgs, config json.RawMessage, e live.Event) bool
}

// TriggerEvents are the event nodes that start triggers from events.
var TriggerEvents = extensions.NewPoint[TriggerEvent]("live.triggerEvent",
	"An app.trigger event node started by committed events: the subjects it starts on and its outputs for an event (forge 07a)")

// TriggerEventOf finds the trigger event of an event node type.
func TriggerEventOf(reg *extensions.Registry, nodeType string) (TriggerEvent, bool) {
	for _, t := range TriggerEvents.All(reg) {
		if t.ID == nodeType {
			return t, true
		}
	}
	return TriggerEvent{}, false
}

// OnEventConfig is On event's config: an event type in the app's own area
// (app_<appId>, what its command graphs commit). Records' events start On
// record changed.
type OnEventConfig struct {
	Type   string   `json:"type"` // the target type
	Events []string `json:"events,omitempty"`
}

const onEventSchema = `{"type":"object","properties":{
  "type":{"type":"string","title":"Type","description":"The target type of the app's own events (area app_<appId>), such as counter"},
  "events":{"type":"array","title":"Events","items":{"type":"string"},"description":"The event types it starts on; every one when empty"}},"required":["type"]}`

// OnScheduleConfig is On schedule's config: an interval, or a time of
// day in a time zone (UTC when none).
type OnScheduleConfig struct {
	Every    string `json:"every,omitempty"`    // a Go duration, at least 1s: 5m, 1h
	At       string `json:"at,omitempty"`       // HH:MM
	TimeZone string `json:"timeZone,omitempty"` // an IANA zone, such as Europe/Paris
}

// OnScheduleSchema is On schedule's config schema.
const OnScheduleSchema = `{"type":"object","properties":{
  "every":{"type":"string","title":"Every","description":"An interval, such as 30s, 5m or 2h (at least 1s)"},
  "at":{"type":"string","title":"At","description":"A time of day, HH:MM, when every is not set"},
  "timeZone":{"type":"string","title":"Time zone","description":"The time zone of at, such as Europe/Paris; UTC when not set"}}}`

// EventSubjects are the subjects of type's events in area, one per event
// type, or every event when events is empty.
func EventSubjects(tenant uint64, area live.Area, typ string, events []string) []string {
	if len(events) == 0 {
		return []string{fmt.Sprintf("evt.%d.%s.%s.*.*", tenant, area, typ)}
	}
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = fmt.Sprintf("evt.%d.%s.%s.*.%s", tenant, area, typ, e)
	}
	return out
}

// eventInputs are an event's outputs: the event as JSON, with its data
// beside it.
func eventInputs(e live.Event) cognition.Values {
	raw, _ := json.Marshal(e)
	var ev map[string]any
	_ = json.Unmarshal(raw, &ev)
	var data any
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &data)
	}
	return cognition.Values{"event": ev, "data": data, "type": e.Type, "target": e.Target.ID}
}

func contributeTriggers(reg *extensions.Registry) {
	TriggerEvents.Register(reg, TriggerEvent{
		Descriptor: extensions.Descriptor{ID: OnEvent, Source: source},
		Subjects: func(args features.ServiceArgs, app string, config json.RawMessage) (live.Area, []string, error) {
			var c OnEventConfig
			if err := json.Unmarshal(config, &c); err != nil {
				return "", nil, fmt.Errorf("On event's config cannot be read: %v", err)
			}
			if !live.ValidToken(c.Type) {
				return "", nil, fmt.Errorf("On event needs a type, not %q", c.Type)
			}
			for _, e := range c.Events {
				if !live.ValidToken(e) {
					return "", nil, fmt.Errorf("%q is not an event type", e)
				}
			}
			area := live.AppArea(app)
			if strings.HasPrefix(c.Type, "_") {
				return "", nil, fmt.Errorf("%q is internal", c.Type)
			}
			return area, EventSubjects(args.Principal.TenantID, area, c.Type, c.Events), nil
		},
		Inputs: func(_ features.ServiceArgs, _ json.RawMessage, e live.Event) (cognition.Values, error) {
			return eventInputs(e), nil
		},
	})
}
