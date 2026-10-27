package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type DeleteResponse struct {
	rest.Status
}

// DeleteResponseFrom converts the service's DeleteResponse; its code is
// carried by the HTTP status rather than the body.
func DeleteResponseFrom(resp chronicleservice.DeleteResponse) DeleteResponse {
	return DeleteResponse{Status: rest.StatusFrom(resp.Common)}
}
