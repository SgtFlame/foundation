package impl

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db       *gorm.DB
	embedder embedservice.Service
	config   config.Reader
	registry *extensions.Registry
}

var log = logging.In("retrieval")
