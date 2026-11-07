package apps

import (
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

// Models are the apps tables, for db.Migrate.
func Models() []any {
	return []any{&AppRow{}}
}
