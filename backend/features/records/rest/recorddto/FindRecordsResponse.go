package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FindRecordsResponse struct {
	rest.Status
	Records []Record  `json:"records"`
	Next    string    `json:"next,omitempty"`
	Plan    data.Plan `json:"plan"`
}

// FindRecordsResponseFrom converts the service's FindResponse.
func FindRecordsResponseFrom(resp recordservice.FindResponse) FindRecordsResponse {
	return FindRecordsResponse{Status: rest.StatusFrom(resp.Common), Records: RecordsFrom(resp.Records), Next: resp.Next, Plan: resp.Plan}
}
