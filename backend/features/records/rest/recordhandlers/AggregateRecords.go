package recordhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/records/rest/recorddto"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// AggregateRecords handles POST /api/v1/records/records/aggregate.
func (rh *RecordHandler) AggregateRecords(c *gin.Context) {
	var req recorddto.AggregateRecordsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}

	resp := rh.service.Aggregate(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), recorddto.AggregateRecordsResponseFrom(resp))
}
