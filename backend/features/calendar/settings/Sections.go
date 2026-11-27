// Package calendarsettings registers the calendar section (forge 28a):
// the cap on one recurrence expansion, the default working hours, the
// mirror window of connected calendars, how long holds last and how often
// CalDAV is polled, read by the calendar's nodes, graphs and connector
// types.
package calendarsettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// SectionID is the calendar section.
const SectionID = "calendar"

// Settings is the calendar section's value.
type Settings struct {
	MaxOccurrences    int            `json:"maxOccurrences"`
	WorkingHours      map[string]any `json:"workingHours"`
	PastDays          int            `json:"pastDays"`
	FutureDays        int            `json:"futureDays"`
	HoldDays          int            `json:"holdDays"`
	CalDAVPollSeconds int            `json:"caldavPollSeconds"`
}

// Defaults are the section's defaults.
var Defaults = Settings{
	MaxOccurrences: 1000,
	WorkingHours: map[string]any{"zone": "UTC", "days": map[string]any{
		"mon": []any{map[string]any{"start": "09:00", "end": "17:00"}},
		"tue": []any{map[string]any{"start": "09:00", "end": "17:00"}},
		"wed": []any{map[string]any{"start": "09:00", "end": "17:00"}},
		"thu": []any{map[string]any{"start": "09:00", "end": "17:00"}},
		"fri": []any{map[string]any{"start": "09:00", "end": "17:00"}},
	}},
	PastDays: 90, FutureDays: 365, HoldDays: 5, CalDAVPollSeconds: 60,
}

const schema = `{
  "type": "object",
  "properties": {
    "maxOccurrences": {"type": "integer", "title": "Occurrences per expansion", "description": "The most occurrences one expansion of a recurring event gives; a rule making more in its window is refused.", "min": 10, "max": 100000},
    "workingHours": {"type": "object", "title": "Default working hours", "description": "Working hours for people who have not set their own: {zone, days: {mon: [{start, end}], …}}."},
    "pastDays": {"type": "integer", "title": "Mirror past days", "description": "How many days back a connected calendar's events are copied in.", "min": 0, "max": 3650},
    "futureDays": {"type": "integer", "title": "Mirror future days", "description": "How many days ahead a connected calendar's events are copied in, and occurrences of recurring events written.", "min": 1, "max": 3650},
    "holdDays": {"type": "integer", "title": "Holds last (days)", "description": "How long proposed meeting times stay held on the calendar before they are released.", "min": 1, "max": 60},
    "caldavPollSeconds": {"type": "integer", "title": "CalDAV poll (s)", "description": "How often a CalDAV calendar is asked for changes.", "min": 15, "max": 86400}
  }
}`

// Contribute registers the calendar section.
func Contribute(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: SectionID, Order: 760, Source: "calendar"},
		Title:       "Calendar",
		Description: "How calendars behave: how far recurring events are expanded, default working hours, how much of a connected calendar is copied in, how long proposed times are held and how often CalDAV is polled.",
		Category:    []string{"Applications", "Calendar"},
		Scopes:      []string{"app", "tenant", "user"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}
