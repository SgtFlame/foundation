package recorddto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

type ListRecordsRequest struct {
	Entity string                     `json:"entity"`
	Where  map[string]json.RawMessage `json:"where,omitempty"`
}

// ToArgs converts the request to the service's ListArgs.
func (r ListRecordsRequest) ToArgs(args features.ServiceArgs) recordservice.ListArgs {
	return recordservice.ListArgs{ServiceArgs: args, Entity: r.Entity, Where: r.Where}
}
