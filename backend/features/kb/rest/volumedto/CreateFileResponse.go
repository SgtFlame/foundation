package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type CreateFileResponse struct {
	rest.Status
}

// CreateFileResponseFrom converts the service's CreateFileResponse; its code is
// carried by the HTTP status rather than the body.
func CreateFileResponseFrom(resp volumeservice.CreateFileResponse) CreateFileResponse {
	return CreateFileResponse{Status: rest.StatusFrom(resp.Common)}
}
