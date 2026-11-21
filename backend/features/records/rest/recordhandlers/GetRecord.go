package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// GetRecord handles POST /api/v1/records/records/get.
func (rh *RecordHandler) GetRecord(c *gin.Context) {
	var req recorddto.GetRecordRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Get(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.GetRecordResponseFrom(resp))
}
