package collabdto

import "github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"

type OpenFileResponse struct {
	Message string `json:"message,omitempty"`
	FileID  uint64 `json:"fileId,string,omitempty"`
}

// OpenFileResponseFrom converts the service's OpenFileResponse; its code is
// carried by the HTTP status rather than the body.
func OpenFileResponseFrom(resp collabservice.OpenFileResponse) OpenFileResponse {
	return OpenFileResponse{Message: resp.Common.Message, FileID: resp.FileID}
}
