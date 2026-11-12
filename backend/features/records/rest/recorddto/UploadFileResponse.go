package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// UploadFileResponse is the reference a file field is then set to. The
// request is multipart: entity, and the file under file.
type UploadFileResponse struct {
	rest.Status
	File *records.FileRef `json:"file,omitempty"`
}

// UploadFileResponseFrom converts the service's UploadResponse.
func UploadFileResponseFrom(resp recordservice.UploadResponse) UploadFileResponse {
	out := UploadFileResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.File.ID != "" {
		out.File = &resp.File
	}
	return out
}
