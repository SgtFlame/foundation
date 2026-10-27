package rest

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/gin-gonic/gin"
)

// Status is embedded in every response DTO: the error text and code of a
// failed request, both empty on success. The HTTP status carries the code
// and the browser already knows the trace, so neither is in the body.
type Status struct {
	Message   string `json:"message,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// StatusFrom takes the status fields of a service response.
func StatusFrom(common features.ServiceResponse) Status {
	return Status{Message: common.Message, ErrorCode: common.ErrorCode}
}

// Bind decodes the JSON body into req and returns the request's ServiceArgs.
// A body that does not decode is answered with 400 and ok is false.
func Bind(c *gin.Context, req any) (args features.ServiceArgs, ok bool) {
	args = Args(c)
	if err := c.ShouldBindJSON(req); err != nil {
		failed := args.Fail(http.StatusBadRequest, logging.In("rest").Code("bad_request").Wrap(err))
		c.JSON(http.StatusBadRequest, StatusFrom(*failed))
		return args, false
	}
	return args, true
}
