// Package logs holds what the logs feature stores besides the logs
// themselves, which live in VictoriaLogs.
package logs

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// UserLogLevel is full logging for one user (documentation/plans/Logging.md):
// until ExpiresAt, the user's browser forwards and the server stores their
// records from Level up. It lives in the application database.
type UserLogLevel struct {
	db.Model
	UserID    uint64 `gorm:"uniqueIndex;not null"`
	Level     string `gorm:"not null"` // debug
	SetBy     uint64 // the administrator who set it
	ExpiresAt time.Time
}

// Models are the logs feature's tables, for the application database.
func Models() []any {
	return []any{&UserLogLevel{}}
}
