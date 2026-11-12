package recorddto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

type ImportRecordsRequest struct {
	Entity string                       `json:"entity"`
	Rows   []map[string]json.RawMessage `json:"rows"`
}

// ToArgs converts the request to the service's ImportArgs.
func (r ImportRecordsRequest) ToArgs(args features.ServiceArgs) recordservice.ImportArgs {
	return recordservice.ImportArgs{ServiceArgs: args, Entity: r.Entity, Rows: r.Rows}
}
