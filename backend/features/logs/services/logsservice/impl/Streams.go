package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// Streams lists the streams of the matching records, or with Field the
// values of one stream field, for the stream picker.
func (s *service) Streams(args logsservice.StreamsArgs) logsservice.StreamsResponse {
	p, f := params(args.ServiceArgs, args.Query, &args.Range)
	if f != nil {
		return logsservice.StreamsResponse{Common: *f.resp}
	}
	path := "/select/logsql/streams"
	if args.Field != "" {
		path = "/select/logsql/stream_field_values"
		p.Set("field", args.Field)
	}
	var body valuesResponse
	if f := s.decode(args.ServiceArgs, path, p, &body); f != nil {
		return logsservice.StreamsResponse{Common: *f.resp, QueryError: f.queryError}
	}
	return logsservice.StreamsResponse{Common: features.OK(), Values: body.list()}
}
