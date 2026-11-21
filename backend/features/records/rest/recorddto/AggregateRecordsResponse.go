package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type AggregateRecordsResponse struct {
	rest.Status
	Groups []data.Group `json:"groups"`
	Plan   data.Plan    `json:"plan"`
}

// AggregateRecordsResponseFrom converts the service's AggregateResponse.
func AggregateRecordsResponseFrom(resp recordservice.AggregateResponse) AggregateRecordsResponse {
	return AggregateRecordsResponse{Status: rest.StatusFrom(resp.Common), Groups: resp.Groups, Plan: resp.Plan}
}
