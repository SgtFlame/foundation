package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// SaveTypeRequest makes or replaces a tenant's or project's entry type.
type SaveTypeRequest struct {
	Type EntryType `json:"type"`
}

// ToArgs converts the request to the service's SaveTypeArgs.
func (r SaveTypeRequest) ToArgs(args features.ServiceArgs) chronicleservice.SaveTypeArgs {
	return chronicleservice.SaveTypeArgs{ServiceArgs: args, Type: r.Type.toType()}
}
