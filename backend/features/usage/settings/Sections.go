// Package usagesettings declares the usage feature's settings section
// (documentation/Usage.md › Built in slice 4) and the Go type it
// decodes into with config.Read.
package usagesettings

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// CaptureID is the debug capture section's ID.
const CaptureID = "usage.capture"

// Capture is the usage.capture section: whether chat calls keep their prompt
// and reply text, for debugging, and for how long.
type Capture struct {
	Enabled        bool `json:"enabled"`
	RetentionHours int  `json:"retentionHours"`
}

// Retention is how long a capture is kept, 24 hours when unset.
func (c Capture) Retention() time.Duration {
	if c.RetentionHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.RetentionHours) * time.Hour
}

// Contribute registers the usage sections on config.Sections.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: CaptureID, Order: 910, Source: "usage"},
		Title:       "Debug capture",
		Description: "Keeps the full prompt and reply of every chat call beside its usage record, to debug what the model was sent. Off by default: captures hold conversation text, and expire after the retention. Logs never hold it.",
		Category:    []string{"System"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(captureSchema),
		Defaults:    json.RawMessage(`{"enabled": false, "retentionHours": 24}`),
		Version:     1,
	})
}

const captureSchema = `{
  "type": "object",
  "properties": {
    "enabled": {"type": "boolean", "title": "Capture chat prompts and replies"},
    "retentionHours": {"type": "integer", "title": "Keep captures for", "min": 1, "max": 168, "unit": "hours"}
  }
}`
