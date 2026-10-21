package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type MakeFolderResponse struct {
	Message string `json:"message,omitempty"`
}

// MakeFolderResponseFrom converts the service's MakeFolderResponse; its code is carried
// by the HTTP status rather than the body.
func MakeFolderResponseFrom(resp volumeservice.MakeFolderResponse) MakeFolderResponse {
	return MakeFolderResponse{Message: resp.Common.Message}
}
