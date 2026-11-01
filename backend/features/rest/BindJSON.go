package rest

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/gin-gonic/gin/binding"
)

// BindJSON decodes and validates a frame's arguments into req as Bind does a
// request body, for handlers on the live connection (live calls); a
// failure is a 400.
func BindJSON(args features.ServiceArgs, raw json.RawMessage, req any) *features.ServiceResponse {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(req); err != nil {
		return args.Fail(http.StatusBadRequest, logging.In("rest").Code("bad_request").Wrap(err))
	}
	if err := binding.Validator.ValidateStruct(req); err != nil {
		return args.Fail(http.StatusBadRequest, logging.In("rest").Code("bad_request").Wrap(err))
	}
	return nil
}
