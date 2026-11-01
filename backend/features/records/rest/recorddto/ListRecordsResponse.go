package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListRecordsResponse struct {
	rest.Status
	Records []Record `json:"records"`
}

// ListRecordsResponseFrom converts the service's ListResponse.
func ListRecordsResponseFrom(resp recordservice.ListResponse) ListRecordsResponse {
	return ListRecordsResponse{Status: rest.StatusFrom(resp.Common), Records: RecordsFrom(resp.Records)}
}
