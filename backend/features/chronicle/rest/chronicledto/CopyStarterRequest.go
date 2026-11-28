package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// CopyStarterRequest copies a starter set's types into a scope; the
// response is a TypesResponse.
type CopyStarterRequest struct {
	Starter string `json:"starter"`
	Scope   string `json:"scope"`
}

// ToArgs converts the request to the service's CopyStarterArgs.
func (r CopyStarterRequest) ToArgs(args features.ServiceArgs) chronicleservice.CopyStarterArgs {
	return chronicleservice.CopyStarterArgs{ServiceArgs: args, Starter: r.Starter, Scope: r.Scope}
}
