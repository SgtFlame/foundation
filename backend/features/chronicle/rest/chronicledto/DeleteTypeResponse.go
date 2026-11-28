package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type DeleteTypeResponse struct {
	rest.Status
}

// DeleteTypeResponseFrom converts the service's DeleteTypeResponse.
func DeleteTypeResponseFrom(resp chronicleservice.DeleteTypeResponse) DeleteTypeResponse {
	return DeleteTypeResponse{Status: rest.StatusFrom(resp.Common)}
}
