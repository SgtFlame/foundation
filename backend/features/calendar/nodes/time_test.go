package calendarnodes_test

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	calendarnodes "github.com/SgtFlame/foundation/backend/features/calendar/nodes"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func node(t *testing.T, reg *extensions.Registry, id string) cognition.NodeType {
	t.Helper()
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %s", id)
	return cognition.NodeType{}
}

func run(t *testing.T, nt cognition.NodeType, in cognition.Values) cognition.Values {
	t.Helper()
	x := &cognition.Exec{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}}
	out, _, err := nt.Run(x, in)
	if err != nil {
		t.Fatalf("%s: %v", nt.ID, err)
	}
	return out
}

func TestTimeNodes(t *testing.T) {
	reg := extensions.NewRegistry()
	calendarnodes.Contribute(reg)

	out := run(t, node(t, reg, calendarnodes.ConvertID), cognition.Values{"local": "2026-03-29T01:30:00", "zone": "Europe/London"})
	if out["instant"] != "2026-03-29T01:30:00Z" || out["local"] != "2026-03-29T02:30:00" || out["abbreviation"] != "BST" {
		t.Fatalf("convert: %v", out)
	}
	out = run(t, node(t, reg, calendarnodes.ExpandID), cognition.Values{
		"start": map[string]any{"local": "2026-03-17T09:00:00", "zone": "Europe/London"}, "rrule": "FREQ=WEEKLY", "durationMinutes": 60.0,
		"window": map[string]any{"from": "2026-03-20T00:00:00Z", "to": "2026-04-03T00:00:00Z"}})
	occ := out["occurrences"].([]any)
	if len(occ) != 2 || occ[1].(map[string]any)["start"] != "2026-03-31T08:00:00Z" {
		t.Fatalf("expand: %v", out)
	}
	out = run(t, node(t, reg, calendarnodes.DescribeID), cognition.Values{"rrule": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"})
	if out["text"] != "every weekday" {
		t.Fatalf("describe: %v", out)
	}
	out = run(t, node(t, reg, calendarnodes.SplitID), cognition.Values{"start": map[string]any{"local": "2026-06-01T09:00:00", "zone": "UTC"},
		"rrule": "FREQ=DAILY;COUNT=10", "recurrenceId": "2026-06-04T09:00:00Z"})
	if out["before"] != "FREQ=DAILY;COUNT=3" || out["after"] != "FREQ=DAILY;COUNT=7" {
		t.Fatalf("split: %v", out)
	}
	busy := run(t, node(t, reg, calendarnodes.BusyID), cognition.Values{"events": []any{
		map[string]any{"start": "2026-06-01T13:00:00Z", "end": "2026-06-01T14:00:00Z"}}})["busy"]
	out = run(t, node(t, reg, calendarnodes.FreeSlotsID), cognition.Values{
		"people":          []any{map[string]any{"id": "a", "busy": busy, "hours": map[string]any{"zone": "UTC", "days": map[string]any{"mon": []any{map[string]any{"start": "13:00", "end": "15:00"}}}}}},
		"durationMinutes": 60.0, "window": map[string]any{"from": "2026-06-01T00:00:00Z", "to": "2026-06-02T00:00:00Z"}})
	slots := out["slots"].([]any)
	if len(slots) != 1 || slots[0].(map[string]any)["start"] != "2026-06-01T14:00:00Z" {
		t.Fatalf("free slots: %v", out)
	}
	if zs := run(t, node(t, reg, calendarnodes.ZonesID), cognition.Values{"search": "london"})["zones"].([]any); len(zs) != 1 {
		t.Fatalf("zones: %v", zs)
	}
}

func TestIcalNodes(t *testing.T) {
	reg := extensions.NewRegistry()
	calendarnodes.ContributeIcal(reg, func() calendarnodes.IcalDeps {
		return calendarnodes.IcalDeps{Read: func(_ context.Context, raw []byte) (ical.Parsed, error) { return ical.Parse(raw) }}
	})
	ev := map[string]any{"iCalUID": "m1@axiom", "title": "Renewal", "start": map[string]any{"local": "2026-12-03T10:00:00", "zone": "America/New_York"},
		"end": map[string]any{"local": "2026-12-03T11:00:00", "zone": "America/New_York"}, "organizer": map[string]any{"email": "tony@example.org"},
		"attendees": []any{map[string]any{"email": "dana@example.com", "response": "needsAction"}}}
	text := run(t, node(t, reg, calendarnodes.ComposeIcsID), cognition.Values{"events": []any{ev}, "method": "REQUEST"})["text"].(string)
	parsed := run(t, node(t, reg, calendarnodes.ParseIcsID), cognition.Values{"text": text})
	if parsed["ok"] != true || parsed["method"] != "REQUEST" {
		t.Fatalf("%v", parsed)
	}
	inv := parsed["events"].([]any)[0]
	reply := run(t, node(t, reg, calendarnodes.ReplyID), cognition.Values{"invitation": inv, "attendee": "dana@example.com", "answer": "tentative"})
	if reply["to"] != "tony@example.org" {
		t.Fatalf("%v", reply)
	}
	applied := run(t, node(t, reg, calendarnodes.ApplyID), cognition.Values{"event": inv, "method": "REPLY", "message": reply["reply"]})
	if applied["applied"] != true || applied["event"].(map[string]any)["attendees"].([]any)[0].(map[string]any)["response"] != "tentative" {
		t.Fatalf("%v", applied)
	}
	refused := run(t, node(t, reg, calendarnodes.ApplyID), cognition.Values{"event": inv, "method": "COUNTER", "message": reply["reply"]})
	if refused["applied"] != false || refused["reason"] != "notApplied" {
		t.Fatalf("%v", refused)
	}
	if bad := run(t, node(t, reg, calendarnodes.ParseIcsID), cognition.Values{"text": "nope"}); bad["ok"] != false {
		t.Fatalf("%v", bad)
	}
}
