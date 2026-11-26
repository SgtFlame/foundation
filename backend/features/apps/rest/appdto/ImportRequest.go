package appdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
)

type ImportRequest struct {
	Document  apps.Document `json:"document"`
	ID        string        `json:"id,omitempty"`        // the app's id here, when not the document's
	Overwrite []string      `json:"overwrite,omitempty"` // <type>:<id> of parts to overwrite
	Keep      []string      `json:"keep,omitempty"`      // <type>:<id> of parts to keep as they are here
	DryRun    bool          `json:"dryRun,omitempty"`
}

// ToArgs converts the request to the service's ImportArgs.
func (r ImportRequest) ToArgs(args features.ServiceArgs) appservice.ImportArgs {
	return appservice.ImportArgs{ServiceArgs: args, Document: r.Document, ID: r.ID, Overwrite: r.Overwrite, Keep: r.Keep, DryRun: r.DryRun}
}
