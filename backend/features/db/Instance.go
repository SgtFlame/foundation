package db

import (
	"fmt"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"gorm.io/gorm"
)

// instanceRow names a database: the followers that write it are named
// after it, so each database file gets every event (forge 21d-3). It is
// the table the large tables have used since slice 52.
type instanceRow struct {
	ID string `gorm:"primaryKey"`
}

func (instanceRow) TableName() string { return "records_db_instance" }

var instances sync.Map // *gorm.DB → string

// Instance is gdb's instance id, made on first use and kept in the
// database itself, so a copy of the file keeps it.
func Instance(gdb *gorm.DB) (string, error) {
	if id, ok := instances.Load(gdb); ok {
		return id.(string), nil
	}
	if err := gdb.AutoMigrate(&instanceRow{}); err != nil {
		return "", err
	}
	var row instanceRow
	if err := gdb.Attrs(instanceRow{ID: fmt.Sprint(features.NewTSID())}).FirstOrCreate(&row).Error; err != nil {
		return "", err
	}
	instances.Store(gdb, row.ID)
	return row.ID, nil
}

// Named is a follower's name for the database it writes: name.<instance>
// (name alone when gdb is nil or has no instance yet).
func Named(name string, gdb *gorm.DB) string {
	if gdb == nil {
		return name
	}
	id, err := Instance(gdb)
	if err != nil {
		return name
	}
	return name + "." + id
}
