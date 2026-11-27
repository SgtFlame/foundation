package usage

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store is where a Recorder writes (forge 21d-2): the usage data
// service's, StoreIn(gdb) where the tenant database is,
// usageservice.Store(proxy) elsewhere.
type Store interface {
	// Append stores records and captures, each once by its ID, so a
	// retried batch changes nothing.
	Append(ctx context.Context, records []Record, captures []Capture) error
	// PurgeCaptures deletes captures made before a time and says how many.
	PurgeCaptures(ctx context.Context, before time.Time) (int64, error)
}

// StoreIn is the Store over a tenant database.
func StoreIn(gdb *gorm.DB) Store { return dbStore{gdb} }

type dbStore struct{ db *gorm.DB }

func (s dbStore) Append(ctx context.Context, records []Record, captures []Capture) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(records) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&records).Error; err != nil {
				return err
			}
		}
		if len(captures) > 0 {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&captures).Error
		}
		return nil
	})
}

func (s dbStore) PurgeCaptures(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("created_at < ?", before).Delete(&Capture{})
	return res.RowsAffected, res.Error
}
