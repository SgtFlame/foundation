package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// FindRecords handles POST /api/v1/records/records/find.
func (rh *RecordHandler) FindRecords(c *gin.Context) {
	var req recorddto.FindRecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Find(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.FindRecordsResponseFrom(resp))
}
