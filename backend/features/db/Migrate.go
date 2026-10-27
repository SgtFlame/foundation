package db

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate creates or updates the tables of models. serve calls it at startup
// with every feature's models.
func Migrate(gdb *gorm.DB, models ...any) error {
	if len(models) == 0 {
		return nil
	}
	if err := gdb.AutoMigrate(models...); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}
