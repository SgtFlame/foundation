package appdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
)

type ExportRequest struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"` // the last published when left out
	Seed    bool   `json:"seed,omitempty"`
}

// ToArgs converts the request to the service's ExportArgs.
func (r ExportRequest) ToArgs(args features.ServiceArgs) appservice.ExportArgs {
	return appservice.ExportArgs{ServiceArgs: args, App: r.ID, Version: r.Version, Seed: r.Seed}
}
