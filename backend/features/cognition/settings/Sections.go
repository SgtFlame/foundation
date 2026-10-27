// Package cognitionsettings declares the cognition section
// (documentation/Cognition.md › Execution): a run's limits and how
// long its trace is kept.
package cognitionsettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ID is the cognition section's ID.
const ID = "cognition"

// Cognition is the cognition section.
type Cognition struct {
	MaxSteps       int `json:"maxSteps"`       // node executions per run
	MaxWallSeconds int `json:"maxWallSeconds"` // a run's wall time
	MaxValueKB     int `json:"maxValueKB"`     // a pin value larger than this is cut short in the trace
	// A trace is kept while it is among its graph's last KeepRuns runs, or
	// younger than KeepDays (Cognition question 5).
	KeepRuns int `json:"keepRuns"`
	KeepDays int `json:"keepDays"`
}

// Defaults are the section's defaults, also used when it cannot be read.
var Defaults = Cognition{MaxSteps: 1000, MaxWallSeconds: 300, MaxValueKB: 256, KeepRuns: 50, KeepDays: 7}

// Contribute registers the cognition section.
func Contribute(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 170, Source: "cognition"},
		Title:       "Cognition",
		Description: "Limits on each run of a Cognition graph, such as the Personal chat mode, and how long run traces are kept for the Log Viewer.",
		Category:    []string{"AI", "Cognition"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}

const schema = `{
  "type": "object",
  "properties": {
    "maxSteps": {"type": "integer", "title": "Steps per run", "min": 1, "max": 100000, "description": "A run that executes more nodes than this fails on the node that went over"},
    "maxWallSeconds": {"type": "integer", "title": "Time per run", "unit": "seconds", "min": 1, "max": 86400},
    "maxValueKB": {"type": "integer", "title": "Largest traced value", "unit": "KB", "min": 1, "max": 16384, "description": "A pin value larger than this is kept cut short in the trace"},
    "keepRuns": {"type": "integer", "title": "Runs kept per graph", "min": 0, "max": 100000, "description": "A run's trace is kept while it is among its graph's latest runs, or younger than the days below"},
    "keepDays": {"type": "integer", "title": "Days kept", "unit": "days", "min": 0, "max": 3650}
  }
}`
