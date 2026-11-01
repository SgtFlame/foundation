package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ImportRecords handles POST /api/v1/records/records/import.
func (rh *RecordHandler) ImportRecords(c *gin.Context) {
	var req recorddto.ImportRecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Import(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.ImportRecordsResponseFrom(resp))
}
