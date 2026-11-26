package recordhandlers

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// UploadFile handles POST /api/v1/records/files/upload: a multipart form
// with the entity type's id under entity and the file under file.
func (rh *RecordHandler) UploadFile(c *gin.Context) {
	args := rest.Args(c)
	header, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, recorddto.UploadFileResponse{Status: rest.Status{Message: "send the file under file, as multipart/form-data", ErrorCode: "bad_request"}})
		return
	}
	f, err := header.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, recorddto.UploadFileResponse{Status: rest.Status{Message: "the file cannot be read", ErrorCode: "bad_request"}})
		return
	}
	defer f.Close()

	resp := rh.service.Upload(recordservice.UploadArgs{ServiceArgs: args, Entity: c.PostForm("entity"), Name: header.Filename,
		ContentType: header.Header.Get("Content-Type"), Content: f})
	c.JSON(int(resp.Common.Code), recorddto.UploadFileResponseFrom(resp))
}
