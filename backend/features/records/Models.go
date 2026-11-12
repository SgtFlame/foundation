package records

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// EntityTypeVersion is one published version of an entity type, copied
// from its published event so that definitions are exported and imported
// with the tenant's other tables (Forge 03-DataModel question 5). Drafts
// stay in the records.entity projection.
type EntityTypeVersion struct {
	db.Model
	db.Tenanted
	EntityID    string `gorm:"index;not null"`
	Version     int    `gorm:"not null"`
	Name        string
	Owner       string
	Definition  string // the EntityType as JSON
	PublishedBy uint64
	PublishedAt time.Time
	Stream      string // the published event's stream and sequence
	Seq         uint64
}

// TableName names the table after the feature.
func (EntityTypeVersion) TableName() string { return "records_entity_versions" }

// Models are the records tables, for db.Migrate.
func Models() []any {
	return []any{&EntityTypeVersion{}}
}
