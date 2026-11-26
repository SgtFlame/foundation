package apps

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// AppRow is an app in the tenant database, one row per app, copied from
// its draft's events by a follower (forge 05a question 1): what versions
// and installs (forge 05b) refer to. The draft itself stays in the
// apps.app projection.
type AppRow struct {
	db.Model
	db.Tenanted
	AppID       string `gorm:"index;not null"`
	Name        string
	Icon        string
	Description string
	CreatedBy   uint64
}

// TableName names the table after the feature.
func (AppRow) TableName() string { return "apps_apps" }

// VersionRow is a published app version in the tenant database, copied by
// a follower from its published event (forge 05b): the whole version as
// JSON, for listing and export (forge 05e). Installs read the apps.version
// projection.
type VersionRow struct {
	db.Model
	db.Tenanted
	AppID       string `gorm:"index;not null"`
	Version     int    `gorm:"not null"`
	Rev         int
	Definition  string // Version as JSON
	PublishedBy uint64
	PublishedAt time.Time
	Stream      string
	Seq         uint64
}

// TableName names the table after the feature.
func (VersionRow) TableName() string { return "apps_versions" }

// InstallRow is an app's install in the tenant database, one row per app
// (forge 05b question 2), kept in step with its install, upgrade and
// uninstall events.
type InstallRow struct {
	db.Model
	db.Tenanted
	AppID       string `gorm:"index;not null"`
	Version     int
	Status      string
	InstalledBy uint64
	InstalledAt time.Time
	ChangedBy   uint64
	ChangedAt   time.Time
}

// TableName names the table after the feature.
func (InstallRow) TableName() string { return "apps_installs" }

// Models are the apps tables, for db.Migrate.
func Models() []any {
	return []any{&AppRow{}, &VersionRow{}, &InstallRow{}}
}
