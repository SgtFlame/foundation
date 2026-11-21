package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// SearchRecords handles POST /api/v1/records/records/search.
func (rh *RecordHandler) SearchRecords(c *gin.Context) {
	var req recorddto.SearchRecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Search(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.SearchRecordsResponseFrom(resp))
}
