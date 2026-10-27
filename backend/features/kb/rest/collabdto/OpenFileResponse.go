package collabdto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type OpenFileResponse struct {
	rest.Status
	FileID   uint64 `json:"fileId,string,omitempty"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

// OpenFileResponseFrom converts the service's OpenFileResponse; its code is
// carried by the HTTP status rather than the body.
func OpenFileResponseFrom(resp collabservice.OpenFileResponse) OpenFileResponse {
	return OpenFileResponse{Status: rest.StatusFrom(resp.Common), FileID: resp.FileID, ReadOnly: resp.ReadOnly}
}
