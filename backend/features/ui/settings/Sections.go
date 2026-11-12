// Package uisettings declares the ui.layouts section: each user's own
// arrangement of an application's docked sub-tabs, per page and layout
// (01-ServerUI question 3).
package uisettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// LayoutsID is the section's ID.
const LayoutsID = "ui.layouts"

const schema = `{
  "type": "object",
  "properties": {
    "layouts": {
      "type": "array", "title": "Arranged sub-tabs", "key": "id",
      "description": "How you arranged an application's docked sub-tabs. Remove one to get the application's own arrangement back.",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "title": "Page and layout", "description": "<appId>/<page>/<layout key>"},
          "model": {"type": "string", "title": "Arrangement", "format": "multiline", "description": "The flexlayout model, as JSON"}
        },
        "required": ["id", "model"]
      }
    }
  }
}`

// Contribute registers the ui.layouts section.
func Contribute(reg *extensions.Registry) {
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: LayoutsID, Order: 950, Source: "ui"},
		Title:       "Application layouts",
		Description: "Your own arrangement of applications' docked sub-tabs, kept for each page. The workbench saves it as you move tabs.",
		Category:    []string{"Workbench", "Application layouts"},
		Scopes:      []string{"user"},
		Schema:      json.RawMessage(schema),
		Defaults:    json.RawMessage(`{"layouts":[]}`),
		Version:     1,
	})
}
