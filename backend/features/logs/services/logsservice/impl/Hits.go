package impl

import (
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// Hits counts the matching records per step, for the histogram.
func (s *service) Hits(args logsservice.HitsArgs) logsservice.HitsResponse {
	p, f := params(args.ServiceArgs, args.Query, &args.Range)
	if f != nil {
		return logsservice.HitsResponse{Common: *f.resp}
	}
	step := args.Step
	if step <= 0 {
		start, end, _ := resolve(args.Range)
		step = end.Sub(start) / 60
	}
	step = max(step.Round(time.Millisecond), time.Millisecond)
	p.Set("step", fmt.Sprintf("%dms", step.Milliseconds()))
	if args.Field != "" {
		p.Set("field", args.Field)
	}

	var body struct {
		Hits []struct {
			Fields     map[string]string `json:"fields"`
			Timestamps []time.Time       `json:"timestamps"`
			Values     []int64           `json:"values"`
			Total      int64             `json:"total"`
		} `json:"hits"`
	}
	if f := s.decode(args.ServiceArgs, "/select/logsql/hits", p, &body); f != nil {
		return logsservice.HitsResponse{Common: *f.resp, QueryError: f.queryError}
	}
	resp := logsservice.HitsResponse{Common: features.OK(), Step: step}
	for _, h := range body.Hits {
		resp.Series = append(resp.Series, logsservice.HitsSeries{Fields: h.Fields, Timestamps: h.Timestamps, Values: h.Values, Total: h.Total})
	}
	return resp
}
