package impl

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	logssettings "github.com/SgtFlame/foundation/backend/features/logs/settings"
	"gorm.io/gorm"
)

type service struct {
	db       *gorm.DB
	victoria *logging.Victoria
	config   config.Reader
	client   *http.Client
	tail     *http.Client
}

var log = logging.In("logs")

// settings is the logging section, or its defaults when it cannot be read.
func (s *service) settings(args features.ServiceArgs) logssettings.Logging {
	if s.config != nil {
		if l, err := config.Read[logssettings.Logging](s.config, args, logssettings.ID); err == nil {
			return l
		}
	}
	return logssettings.Logging{}
}

// failure is how a VictoriaLogs call failed: the response, and the parse
// error's position when the query did not parse.
type failure struct {
	resp       *features.ServiceResponse
	queryError *logsservice.QueryError
}

// scope checks that args' principal may read logs, and is the filter that
// keeps them to their tenant unless they may read all logs.
func scope(args features.ServiceArgs) (string, *failure) {
	resource := authz.Tenant(args.Principal)
	if authz.Check(args.Context(), args.Principal, "logs.readAll", resource).Allowed {
		return "", nil
	}
	const action = "logs.read"
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return "", &failure{resp: args.Fail(authz.Status, err)}
	}
	return `tenant.id:="` + strconv.FormatUint(args.Principal.TenantID, 10) + `"`, nil
}

// params are the arguments every query endpoint takes: the query, the
// tenant scope and, when r is not nil, its time range.
func params(args features.ServiceArgs, query string, r *logsservice.Range) (url.Values, *failure) {
	extra, f := scope(args)
	if f != nil {
		return nil, f
	}
	if strings.TrimSpace(query) == "" {
		query = "*"
	}
	p := url.Values{"query": {query}}
	if extra != "" {
		p.Set("extra_filters", extra)
	}
	if r != nil {
		start, end, err := resolve(*r)
		if err != nil {
			return nil, &failure{resp: args.Fail(http.StatusBadRequest, err)}
		}
		p.Set("start", start.Format(time.RFC3339Nano))
		p.Set("end", end.Format(time.RFC3339Nano))
	}
	return p, nil
}

// resolve fills a Range's defaults and checks its span.
func resolve(r logsservice.Range) (start, end time.Time, err error) {
	start, end = r.Start, r.End
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-time.Hour)
	}
	switch {
	case !start.Before(end):
		err = log.Code("bad_range").Errorf("the range starts at %s, not before its end %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	case end.Sub(start) > logsservice.MaxRange:
		err = log.Code("range_too_long").Errorf("the range spans %s, more than the %d days allowed", end.Sub(start).Round(time.Hour), int(logsservice.MaxRange.Hours()/24))
	}
	return start, end, err
}

// call posts p to a VictoriaLogs endpoint. A query that does not parse fails
// with 400 bad_query; storage that is off or unreachable with 503.
func (s *service) call(args features.ServiceArgs, client *http.Client, path string, p url.Values) (*http.Response, *failure) {
	target := s.target()
	if !target.Configured() {
		err := log.Code("logs_not_configured").Public("Log storage is not configured. Turn it on in Settings › System › Logging.").New("no VictoriaLogs target")
		return nil, &failure{resp: args.Fail(http.StatusServiceUnavailable, err)}
	}
	req, err := http.NewRequestWithContext(args.Context(), http.MethodPost, target.URL+path, strings.NewReader(p.Encode()))
	if err != nil {
		return nil, &failure{resp: args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := target.Do(client, req)
	if err != nil {
		err = log.Code("logs_unavailable").Public("Log storage cannot be reached").With("path", path).Wrap(err)
		return nil, &failure{resp: args.Fail(http.StatusServiceUnavailable, err)}
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusBadRequest {
		qe := parseError(text)
		return nil, &failure{resp: args.Fail(http.StatusBadRequest, log.Code("bad_query").Public(qe.Message).New(text)), queryError: &qe}
	}
	err = log.Code("logs_unavailable").Public("Log storage failed").With("path", path, "status", resp.StatusCode).New(text)
	return nil, &failure{resp: args.Fail(http.StatusBadGateway, err)}
}

func (s *service) target() logging.Target {
	if s.victoria == nil {
		return logging.Target{}
	}
	return s.victoria.Target()
}

// decode reads a JSON response into out.
func (s *service) decode(args features.ServiceArgs, path string, p url.Values, out any) *failure {
	resp, f := s.call(args, s.client, path, p)
	if f != nil {
		return f
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return &failure{resp: args.Fail(http.StatusBadGateway, log.Code("logs_unavailable").With("path", path).Wrap(err))}
	}
	return nil
}

// VictoriaLogs reports "cannot parse `query` arg [<query>]: <reason>;
// context: [<the query up to where parsing stopped>]".
var (
	parsePrefix  = regexp.MustCompile("^cannot parse `query` arg \\[.*?\\]: ")
	parseContext = regexp.MustCompile(`(?s); context: \[(.*)\]$`)
)

func parseError(text string) logsservice.QueryError {
	qe := logsservice.QueryError{Message: text, Offset: -1}
	if m := parseContext.FindStringSubmatchIndex(text); m != nil {
		qe.Offset = m[3] - m[2]
		text = text[:m[0]]
	}
	qe.Message = parsePrefix.ReplaceAllString(text, "")
	return qe
}

// valuesResponse is the shape of field_names, field_values and streams.
type valuesResponse struct {
	Values []struct {
		Value string `json:"value"`
		Hits  int64  `json:"hits"`
	} `json:"values"`
}

func (v valuesResponse) list() []logsservice.ValueHits {
	out := make([]logsservice.ValueHits, 0, len(v.Values))
	for _, x := range v.Values {
		out = append(out, logsservice.ValueHits{Value: x.Value, Hits: x.Hits})
	}
	return out
}
