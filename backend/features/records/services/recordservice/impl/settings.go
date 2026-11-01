package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
)

func recordsSettings(s *service, args features.ServiceArgs) recordssettings.Records {
	if s.deps.Settings == nil {
		return recordssettings.Defaults
	}
	return s.deps.Settings(args)
}
