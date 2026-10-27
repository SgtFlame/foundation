package impl

import (
	"bufio"
	"cmp"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

var sortPipe = regexp.MustCompile(`\|\s*(sort|order)\b`)

// Query runs a LogsQL query over the range, newest records first.
func (s *service) Query(args logsservice.QueryArgs) logsservice.QueryResponse {
	limit := args.Limit
	switch {
	case limit <= 0:
		limit = 500
	case limit > 5000:
		limit = 5000
	}
	p, f := params(args.ServiceArgs, args.Query, &args.Range)
	if f != nil {
		return logsservice.QueryResponse{Common: *f.resp}
	}
	// One more than the limit tells whether there were more
	p.Set("limit", strconv.Itoa(limit+1))
	resp, f := s.call(args.ServiceArgs, s.client, "/select/logsql/query", p)
	if f != nil {
		return logsservice.QueryResponse{Common: *f.resp, QueryError: f.queryError}
	}
	defer resp.Body.Close()

	var out logsservice.QueryResponse
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var row logsservice.Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return logsservice.QueryResponse{Common: *args.Fail(http.StatusBadGateway, log.Code("logs_unavailable").Wrap(err))}
		}
		out.Rows = append(out.Rows, row)
	}
	if err := scanner.Err(); err != nil {
		return logsservice.QueryResponse{Common: *args.Fail(http.StatusBadGateway, log.Code("logs_unavailable").Wrap(err))}
	}
	// VictoriaLogs returns the newest records but in no particular order, unless
	// the query sorts them itself; RFC 3339 in UTC sorts as text
	if !sortPipe.MatchString(args.Query) {
		slices.SortStableFunc(out.Rows, func(a, b logsservice.Row) int { return cmp.Compare(b["_time"], a["_time"]) })
	}
	if len(out.Rows) > limit {
		out.Rows, out.Limited = out.Rows[:limit], true
	}
	out.Common = features.OK()
	return out
}
