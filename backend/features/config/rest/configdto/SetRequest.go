package configdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

type SetRequest struct {
	Section  string          `json:"section" binding:"required"`
	Scope    string          `json:"scope"`
	Patch    json.RawMessage `json:"patch" binding:"required"` // a JSON merge patch; null inherits a key again
	Revision int64           `json:"revision"`                 // the View's; a stale one fails with 409
}

// ToArgs converts the request to the service's SetArgs.
func (r SetRequest) ToArgs(args features.ServiceArgs) configservice.SetArgs {
	return configservice.SetArgs{ServiceArgs: args, Section: r.Section, Scope: configservice.Scope(r.Scope), Patch: r.Patch, Revision: r.Revision}
}
