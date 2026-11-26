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
	ScanLimit         int `json:"scanLimit"`         // records a query reads without an index
	ImportConcurrency int `json:"importConcurrency"` // record.create commands records.import sends at once
	// DebounceMs is how long a query that runs again (expansions,
	// aggregates) waits after a change for more before it does
	DebounceMs int `json:"debounceMs"`
	MaxFileMB  int `json:"maxFileMB"` // the largest file a file field takes
}

// Defaults are the section's defaults, also used when it cannot be read.
var Defaults = Records{ScanLimit: 10000, ImportConcurrency: 8, DebounceMs: 100, MaxFileMB: 25}

const schema = `{
  "type": "object",
  "properties": {
    "scanLimit": {"type": "integer", "title": "Scan limit", "description": "How many records of one entity type a query reads without an index (or while walking one for its order). A scan stops there and says so; a live list over more fails. Declare an index for what is filtered or sorted by.", "min": 100, "max": 1000000},
    "importConcurrency": {"type": "integer", "title": "Import concurrency", "description": "How many records an import creates at once.", "min": 1, "max": 64},
    "debounceMs": {"type": "integer", "title": "Re-run delay (ms)", "description": "How long a live query that runs again on every change (one with expansions, or an aggregate) waits after a change for others, so a burst of changes runs it once.", "min": 10, "max": 10000},
    "maxFileMB": {"type": "integer", "title": "Largest file (MB)", "description": "The largest file a record's file field takes; larger uploads are refused.", "min": 1, "max": 1024}
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
