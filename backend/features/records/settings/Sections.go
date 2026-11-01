// Package recordssettings declares the records section (documentation/
// Records.md › Settings): the record store's limits.
package recordssettings

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ID is the records section's ID.
const ID = "records"

// Records is the records section.
type Records struct {
	ScanLimit         int `json:"scanLimit"`         // records a scanning records.list may read
	ImportConcurrency int `json:"importConcurrency"` // record.create commands records.import sends at once
}

// Defaults are the section's defaults, also used when it cannot be read.
var Defaults = Records{ScanLimit: 10000, ImportConcurrency: 8}

const schema = `{
  "type": "object",
  "properties": {
    "scanLimit": {"type": "integer", "title": "Scan limit", "description": "How many records of one entity type a list may read without an index. A list over more fails until indexes come.", "min": 100, "max": 1000000},
    "importConcurrency": {"type": "integer", "title": "Import concurrency", "description": "How many records an import creates at once.", "min": 1, "max": 64}
  }
}`

// Contribute registers the records section.
func Contribute(reg *extensions.Registry) {
	defaults, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: ID, Order: 910, Source: "records"},
		Title:       "Records",
		Description: "Limits of the record store that applications keep their data in.",
		Category:    []string{"System", "Records"},
		Scopes:      []string{"app", "tenant"},
		Schema:      json.RawMessage(schema),
		Defaults:    defaults,
		Version:     1,
	})
}
