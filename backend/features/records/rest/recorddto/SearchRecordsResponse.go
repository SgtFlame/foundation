package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type SearchRecordsResponse struct {
	rest.Status
	Records []Record `json:"records"` // best first, each with its _score
}

// SearchRecordsResponseFrom converts the service's SearchResponse.
func SearchRecordsResponseFrom(resp recordservice.SearchResponse) SearchRecordsResponse {
	return SearchRecordsResponse{Status: rest.StatusFrom(resp.Common), Records: RecordsFrom(resp.Records)}
}
