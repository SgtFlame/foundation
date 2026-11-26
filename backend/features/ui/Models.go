package ui

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// PrefabRow is a prefab's last published version, one row per prefab, so
// export and import find prefabs with the tenant's other definitions
// (forge 04a question 3). Drafts stay in the ui.prefab projection.
type PrefabRow struct {
	db.Model
	db.Tenanted
	PrefabID    string `gorm:"index;not null"`
	Version     int    `gorm:"not null"`
	Name        string
	Owner       string
	Definition  string // the Prefab as JSON
	PublishedBy uint64
	PublishedAt time.Time
}

// TableName names the table after the feature.
func (PrefabRow) TableName() string { return "ui_prefabs" }

// PrefabVersion is one published version of a prefab, copied from its
// published event.
type PrefabVersion struct {
	db.Model
	db.Tenanted
	PrefabID    string `gorm:"index;not null"`
	Version     int    `gorm:"not null"`
	Name        string
	Owner       string
	Definition  string // the Prefab as JSON
	PublishedBy uint64
	PublishedAt time.Time
	Stream      string // the published event's stream and sequence
	Seq         uint64
}

// TableName names the table after the feature.
func (PrefabVersion) TableName() string { return "ui_prefab_versions" }

// Models are the ui tables, for db.Migrate.
func Models() []any {
	return []any{&PrefabRow{}, &PrefabVersion{}}
}
