package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type MakeFolderResponse struct {
	rest.Status
}

// MakeFolderResponseFrom converts the service's MakeFolderResponse; its code is carried
// by the HTTP status rather than the body.
func MakeFolderResponseFrom(resp volumeservice.MakeFolderResponse) MakeFolderResponse {
	return MakeFolderResponse{Status: rest.StatusFrom(resp.Common)}
}
