// Package livesettings declares the live section (documentation/Live.md ›
// Settings): limits per live connection.
package livesettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ID is the live section's ID.
const ID = "live"

// Live is the live section.
type Live struct {
	MaxSubscriptions int `json:"maxSubscriptions"` // open queries one connection may hold
}

// Defaults are the section's defaults, also used when it cannot be read.
var Defaults = Live{MaxSubscriptions: 500}

const schema = `{
  "type": "object",
  "properties": {
    "maxSubscriptions": {"type": "integer", "title": "Subscriptions per connection", "description": "How many queries one browser window may hold open at once; a page binds one per query it shows.", "min": 10, "max": 10000}
  }
}`

// Contribute registers the live section.
func Contribute(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 900, Source: "live"},
		Title:       "Live connection",
		Description: "Limits of the live connection each browser window holds to the server, which carries commands, subscriptions, presence and UI sessions.",
		Category:    []string{"System", "Live connection"},
		Scopes:      []string{"app"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}
