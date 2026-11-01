package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// FindRecordsRequest is a query: entity, where, filter, sort, limit,
// cursor and fields (data.Query).
type FindRecordsRequest struct {
	data.Query
}

// ToArgs converts the request to the service's FindArgs.
func (r FindRecordsRequest) ToArgs(args features.ServiceArgs) recordservice.FindArgs {
	return recordservice.FindArgs{ServiceArgs: args, Query: r.Query}
}
