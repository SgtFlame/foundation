package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type GetRecordResponse struct {
	rest.Status
	Record Record `json:"record"` // null when there is none
}

// GetRecordResponseFrom converts the service's GetResponse.
func GetRecordResponseFrom(resp recordservice.GetResponse) GetRecordResponse {
	return GetRecordResponse{Status: rest.StatusFrom(resp.Common), Record: Record(resp.Record)}
}
