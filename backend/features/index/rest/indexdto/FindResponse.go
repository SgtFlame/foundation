package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FindResponse struct {
	rest.Status
	Hits []Hit `json:"hits"`
}

// FindResponseFrom converts the service's FindResponse; its code is carried by
// the HTTP status rather than the body.
func FindResponseFrom(resp indexservice.FindResponse) FindResponse {
	hits := make([]Hit, len(resp.Hits))
	for i, h := range resp.Hits {
		hits[i] = HitFrom(h)
	}
	return FindResponse{Status: rest.StatusFrom(resp.Common), Hits: hits}
}
