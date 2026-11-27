package conversation

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CallsIn is a tools.CallStore over the tenant database, for the
// conversation data service itself (forge 21d-2): a save is an upsert by
// the record's ID.
func CallsIn(gdb *gorm.DB) tools.CallStore { return dbCalls{gdb} }

type dbCalls struct{ db *gorm.DB }

func (c dbCalls) SaveCall(args features.ServiceArgs, rec *tools.ToolCall) error {
	return c.db.WithContext(args.Context()).Clauses(clause.OnConflict{UpdateAll: true}).Create(rec).Error
}
