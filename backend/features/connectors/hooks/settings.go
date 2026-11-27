package hooks

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// SectionID is the hooks' settings section.
const SectionID = "connectors.hooks"

// Limits are the hook endpoint's limits.
type Limits struct {
	MaxBodyKB     int     `json:"maxBodyKB"`     // the largest body taken
	RatePerSecond float64 `json:"ratePerSecond"` // callbacks a second per hook, on average
	Burst         int     `json:"burst"`         // callbacks one hook may send at once
}

// Defaults are the limits when none are set.
var Defaults = Limits{MaxBodyKB: 1024, RatePerSecond: 20, Burst: 100}

func (l Limits) withDefaults() Limits {
	if l.MaxBodyKB <= 0 {
		l.MaxBodyKB = Defaults.MaxBodyKB
	}
	if l.RatePerSecond <= 0 {
		l.RatePerSecond = Defaults.RatePerSecond
	}
	if l.Burst <= 0 {
		l.Burst = Defaults.Burst
	}
	return l
}

const schema = `{
  "type": "object",
  "properties": {
    "maxBodyKB": {"type": "integer", "title": "Largest callback (KB)", "description": "A callback with a larger body is refused with 413, before its signature is checked.", "min": 16, "max": 16384},
    "ratePerSecond": {"type": "number", "title": "Callbacks a second per hook", "description": "On average; past it a hook's callbacks are refused with 429 and the service sends them again later.", "min": 1, "max": 10000},
    "burst": {"type": "integer", "title": "Callbacks at once per hook", "description": "How many callbacks one hook may send at once before the rate applies.", "min": 1, "max": 100000}
  }
}`

// ContributeSettings registers the connectors.hooks section.
func ContributeSettings(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: SectionID, Order: 905, Source: "connectors"},
		Title:       "Connector hooks",
		Description: "Limits of the endpoint where outside services push events for a connection (webhooks).",
		Category:    []string{"System", "Connectors"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}
