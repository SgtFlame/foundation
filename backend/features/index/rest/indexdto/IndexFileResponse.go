package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type IndexFileResponse struct {
	rest.Status
	Chunks   int `json:"chunks"`
	Embedded int `json:"embedded"`
}

// IndexFileResponseFrom converts the service's IndexFileResponse; its code is
// carried by the HTTP status rather than the body.
func IndexFileResponseFrom(resp indexservice.IndexFileResponse) IndexFileResponse {
	return IndexFileResponse{Status: rest.StatusFrom(resp.Common), Chunks: resp.Chunks, Embedded: resp.Embedded}
}
