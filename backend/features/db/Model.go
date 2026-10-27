package db

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"gorm.io/gorm"
)

// Model is the base of every table: a TSID primary key, never an
// auto-increment number, plus the times GORM maintains.
type Model struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BeforeCreate assigns a new TSID unless the caller set the ID.
func (m *Model) BeforeCreate(*gorm.DB) error {
	if m.ID == 0 {
		m.ID = features.NewTSID()
	}
	return nil
}
