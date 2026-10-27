package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type SharesResponse struct {
	rest.Status
	Shares []Share `json:"shares"`
}

// SharesResponseFrom converts the service's SharesResponse; its code is
// carried by the HTTP status rather than the body.
func SharesResponseFrom(resp chronicleservice.SharesResponse) SharesResponse {
	out := SharesResponse{Status: rest.StatusFrom(resp.Common), Shares: make([]Share, len(resp.Shares))}
	for i, s := range resp.Shares {
		out.Shares[i] = ShareFrom(s)
	}
	return out
}
