package impl

import (
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db *gorm.DB
}

var log = logging.In("usage")
