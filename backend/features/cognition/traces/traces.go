// Package traces keeps Cognition's run traces in the tenant database: the
// cognition data service's storage (forge 21d-2). The engine keeps no
// handle: it writes through an engine.Traces, this package's In in the
// process holding the database, cognitionservice.Traces elsewhere.
package traces

import (
	"context"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store is the traces over a tenant database.
type Store struct{ db *gorm.DB }

// In is the traces kept in gdb; nil when gdb is nil (no traces kept).
func In(gdb *gorm.DB) *Store {
	if gdb == nil {
		return nil
	}
	return &Store{gdb}
}

// Begin stores a run's record as it starts.
func (s *Store) Begin(ctx context.Context, rec cognition.Run) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&rec).Error
}

// Save stores a finished run and its steps, replacing any earlier copy,
// so a retried save keeps it once.
func (s *Store) Save(ctx context.Context, rec cognition.Run, steps []cognition.Step) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", rec.ID).Delete(&cognition.Step{}).Error; err != nil {
			return err
		}
		if len(steps) > 0 {
			if err := tx.CreateInBatches(steps, 100).Error; err != nil {
				return err
			}
		}
		return tx.Save(&rec).Error
	})
}

// Get is a run of the tenant with its steps; found is false when it is
// not kept (expired, or never traced).
func (s *Store) Get(ctx context.Context, tenant, runID uint64) (run cognition.Run, steps []cognition.Step, found bool, err error) {
	res := s.db.WithContext(ctx).Scopes(db.InTenant(tenant)).Limit(1).Find(&run, runID)
	if res.Error != nil || res.RowsAffected == 0 {
		return run, nil, false, res.Error
	}
	err = s.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq").Find(&steps).Error
	return run, steps, err == nil, err
}

// Purge deletes the traces that are neither among their graph's last
// keepRuns runs (per tenant) nor younger than keepDays.
func (s *Store) Purge(ctx context.Context, keepRuns, keepDays int) error {
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM cognition_runs WHERE started_at < ? AND id NOT IN (
			SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id, graph_id ORDER BY started_at DESC, id DESC) AS n FROM cognition_runs) WHERE n <= ?)`,
			cutoff, keepRuns).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM cognition_run_steps WHERE run_id NOT IN (SELECT id FROM cognition_runs)`).Error
	})
}
