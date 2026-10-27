package usagedto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

type RecordsResponse struct {
	rest.Status
	Records []UsageRecord `json:"records,omitempty"`
	Total   int64         `json:"total"`
}

// RecordsResponseFrom converts the service's RecordsResponse; its code is
// carried by the HTTP status rather than the body.
func RecordsResponseFrom(resp usageservice.RecordsResponse) RecordsResponse {
	out := RecordsResponse{Status: rest.StatusFrom(resp.Common), Total: resp.Total}
	for _, r := range resp.Records {
		out.Records = append(out.Records, UsageRecordFrom(r))
	}
	return out
}
