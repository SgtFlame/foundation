package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type LinkResponse struct {
	rest.Status
	Links []Link `json:"links"` // to and from fromId, after the change
}

// LinkResponseFrom converts the service's LinkResponse; its code is carried
// by the HTTP status rather than the body.
func LinkResponseFrom(resp chronicleservice.LinkResponse) LinkResponse {
	return LinkResponse{Status: rest.StatusFrom(resp.Common), Links: linksFrom(resp.Links)}
}
