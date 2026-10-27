// Package config holds what features need to declare, store and read
// settings (documentation/Configuration.md): the config.section
// extension point, the schema subset sections are validated against, the
// Configuration tables, secret encryption, and typed reads. configservice
// serves them.
package config

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Section is a named group of settings, declared in the same shape as the
// frontend's configuration contributions. Descriptor.ID is the dotted,
// owner-prefixed section ID, e.g. "ai.chat.levels". A section's value is
// always a JSON object.
type Section struct {
	extensions.Descriptor
	Title       string
	Description string
	Category    []string        // the Settings tree path, e.g. ["AI", "Chat"]
	Scopes      []string        // of "app", "tenant", "user"
	Schema      json.RawMessage // the JSON-Schema subset of Schema.go; kept raw so its key order reaches the form
	Defaults    json.RawMessage
	Version     int
	// Migrate, when set, brings a value stored at an older Version up to date.
	Migrate func(old map[string]any, fromVersion int) (map[string]any, error)
	// Check, when set, validates what the schema cannot express, such as
	// "exactly one level is the default", after the schema has passed.
	Check func(value map[string]any) []FieldError
}

// Sections is the point backend settings sections are registered on.
var Sections = extensions.NewPoint[Section]("config.section", "Settings sections the backend reads and validates")
