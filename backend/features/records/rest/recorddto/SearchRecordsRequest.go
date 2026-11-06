package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// SearchRecordsRequest is a search: entity (all when left out), text,
// limit and lexicalSemantic (data.SearchQuery).
type SearchRecordsRequest struct {
	data.SearchQuery
}

// ToArgs converts the request to the service's SearchArgs.
func (r SearchRecordsRequest) ToArgs(args features.ServiceArgs) recordservice.SearchArgs {
	return recordservice.SearchArgs{ServiceArgs: args, Query: r.SearchQuery}
}
