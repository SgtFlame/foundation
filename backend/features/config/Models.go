package config

import (
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Configuration holds one section's overrides at one scope. Value holds only
// the keys this scope overrides, secrets as {"$secret": <ciphertext>}.
type Configuration struct {
	db.Model
	Scope         string `gorm:"uniqueIndex:idx_configuration_scope_section;not null"` // app, tenant:<id> or user:<id>
	Section       string `gorm:"uniqueIndex:idx_configuration_scope_section;not null"`
	Value         string `gorm:"not null"` // JSON
	SchemaVersion int
	Revision      int64
	UpdatedBy     uint64
}

// ConfigurationRevision is the append-only history of a Configuration.
type ConfigurationRevision struct {
	db.Model
	ConfigurationID uint64 `gorm:"index;not null"`
	Revision        int64
	Value           string
	UpdatedBy       uint64
}

// Models are the config feature's tables, for db.Migrate.
func Models() []any {
	return []any{&Configuration{}, &ConfigurationRevision{}}
}
