// Package config holds what features need to declare, store and read
// settings (documentation/Configuration.md): the config.section
// extension point, the schema subset sections are validated against, the
// Configuration tables, secret encryption, and typed reads. configservice
// serves them.
package config

import (
	"context"
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

// SectionSource gives sections that exist per tenant rather than per
// server, such as an installed app's (forge 05c: app.<appId>). Its
// sections resolve, show and validate like registered ones, but their
// resolved values are not cached, since the source may change them.
type SectionSource struct {
	extensions.Descriptor
	// Sections are the tenant's sections from this source.
	Sections func(ctx context.Context, tenant uint64) []Section
}

// SectionSources is the point per-tenant section sources are registered on.
var SectionSources = extensions.NewPoint[SectionSource]("config.sectionSource", "Sources of settings sections that exist per tenant, such as installed apps'")

// SectionsFor are the registered sections, then those the sources give
// the tenant.
func SectionsFor(ctx context.Context, reg *extensions.Registry, tenant uint64) (all []Section, sourced int) {
	all = Sections.All(reg)
	n := len(all)
	for _, s := range SectionSources.All(reg) {
		all = append(all, s.Sections(ctx, tenant)...)
	}
	return all, len(all) - n
}
