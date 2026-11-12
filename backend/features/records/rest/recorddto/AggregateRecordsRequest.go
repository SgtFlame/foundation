package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// AggregateRecordsRequest is an aggregation: entity, where, filter,
// groupBy and aggregates (data.Aggregation).
type AggregateRecordsRequest struct {
	data.Aggregation
}

// ToArgs converts the request to the service's AggregateArgs.
func (r AggregateRecordsRequest) ToArgs(args features.ServiceArgs) recordservice.AggregateArgs {
	return recordservice.AggregateArgs{ServiceArgs: args, Aggregation: r.Aggregation}
}
