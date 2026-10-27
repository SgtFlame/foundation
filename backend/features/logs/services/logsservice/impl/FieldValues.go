package impl

import (
	"net/http"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// FieldValues lists a field's values in the matching records with their
// counts, for autocomplete and facets.
func (s *service) FieldValues(args logsservice.FieldValuesArgs) logsservice.FieldValuesResponse {
	if args.Field == "" {
		return logsservice.FieldValuesResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("no_field").New("no field named"))}
	}
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = 100
	case limit > 1000:
		limit = 1000
	}
	p, f := params(args.ServiceArgs, args.Query, &args.Range)
	if f != nil {
		return logsservice.FieldValuesResponse{Common: *f.resp}
	}
	p.Set("field", args.Field)
	p.Set("limit", strconv.Itoa(limit))
	var body valuesResponse
	if f := s.decode(args.ServiceArgs, "/select/logsql/field_values", p, &body); f != nil {
		return logsservice.FieldValuesResponse{Common: *f.resp, QueryError: f.queryError}
	}
	return logsservice.FieldValuesResponse{Common: features.OK(), Values: body.list()}
}
