package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type RemoveFolderResponse struct {
	Message string `json:"message,omitempty"`
}

// RemoveFolderResponseFrom converts the service's RemoveFolderResponse; its code is carried
// by the HTTP status rather than the body.
func RemoveFolderResponseFrom(resp volumeservice.RemoveFolderResponse) RemoveFolderResponse {
	return RemoveFolderResponse{Message: resp.Common.Message}
}
