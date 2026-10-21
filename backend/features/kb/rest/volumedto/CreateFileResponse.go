package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type CreateFileResponse struct {
	Message string `json:"message,omitempty"`
}

// CreateFileResponseFrom converts the service's CreateFileResponse; its code is
// carried by the HTTP status rather than the body.
func CreateFileResponseFrom(resp volumeservice.CreateFileResponse) CreateFileResponse {
	return CreateFileResponse{Message: resp.Common.Message}
}
