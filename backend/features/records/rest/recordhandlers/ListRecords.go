package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// ListRecords handles POST /api/v1/records/records/list.
func (rh *RecordHandler) ListRecords(c *gin.Context) {
	var req recorddto.ListRecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.List(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.ListRecordsResponseFrom(resp))
}
