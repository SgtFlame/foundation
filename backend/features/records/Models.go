package records

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// EntityTypeVersion is one published version of an entity type, copied
// from its published event so that definitions are exported and imported
// with the tenant's other tables (Forge 003-DataModel question 5). Drafts
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

// ProjectedTable is the state of one entity type's table in the tenant
// database (Records.md › Built in slice 52): which table holds its rows,
// the published version and columns it was built for, and the last event
// of the type it reflects. Every write to the table updates it in the same
// transaction, so an event at or below Seq is never applied twice.
type ProjectedTable struct {
	TenantID uint64 `gorm:"primaryKey;autoIncrement:false"`
	Entity   string `gorm:"primaryKey"`
	Name     string `gorm:"not null"` // the table: rec_<tenant>_<tsid>, a new one at each rebuild
	Version  int    // the published version it follows
	Schema   string // its columns, as JSON (data.tableSchema)
	Seq      uint64 // the rec stream sequence of the last event of the type applied
}

// TableName names the table after the feature.
func (ProjectedTable) TableName() string { return "records_tables" }

// DatabaseInstance names this database: the followers that write its
// tables are named after it, so each database (one SQLite file per process,
// or a shared one) gets every row (forge 07k question 2).
type DatabaseInstance struct {
	ID string `gorm:"primaryKey"`
}

// TableName names the table after the feature.
func (DatabaseInstance) TableName() string { return "records_db_instance" }

// Models are the records tables, for db.Migrate.
func Models() []any {
	return []any{&EntityTypeVersion{}, &ProjectedTable{}, &DatabaseInstance{}}
}
