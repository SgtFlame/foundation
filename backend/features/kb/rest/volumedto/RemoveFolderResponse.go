package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type RemoveFolderResponse struct {
	rest.Status
}

// RemoveFolderResponseFrom converts the service's RemoveFolderResponse; its code is carried
// by the HTTP status rather than the body.
func RemoveFolderResponseFrom(resp volumeservice.RemoveFolderResponse) RemoveFolderResponse {
	return RemoveFolderResponse{Status: rest.StatusFrom(resp.Common)}
}
