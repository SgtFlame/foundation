package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// FieldNames lists the fields of the matching records, for autocomplete.
func (s *service) FieldNames(args logsservice.FieldNamesArgs) logsservice.FieldNamesResponse {
	p, f := params(args.ServiceArgs, args.Query, &args.Range)
	if f != nil {
		return logsservice.FieldNamesResponse{Common: *f.resp}
	}
	var body valuesResponse
	if f := s.decode(args.ServiceArgs, "/select/logsql/field_names", p, &body); f != nil {
		return logsservice.FieldNamesResponse{Common: *f.resp, QueryError: f.queryError}
	}
	return logsservice.FieldNamesResponse{Common: features.OK(), Names: body.list()}
}
