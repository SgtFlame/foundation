package impl

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// MaxIngest is the most records one Ingest takes.
const MaxIngest = 200

// maxText bounds each text field of a forwarded record.
const maxText = 8 * 1024

// Ingest stores records forwarded from the principal's browser, with the
// server's field names and source=browser. The user and tenant come from the
// principal, never from the records. Records below the browser level are
// dropped, and nothing is stored while storage is off.
func (s *service) Ingest(args logsservice.IngestArgs) logsservice.IngestResponse {
	const action = "logs.ingest"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return logsservice.IngestResponse{Common: *args.Fail(authz.Status, err)}
	}
	if len(args.Records) > MaxIngest {
		err := log.Code("too_many_records").Errorf("%d records in one batch, more than %d", len(args.Records), MaxIngest)
		return logsservice.IngestResponse{Common: *args.Fail(http.StatusBadRequest, err)}
	}
	if s.victoria == nil || !s.victoria.Target().Configured() {
		return logsservice.IngestResponse{Common: features.OK()}
	}

	lowest := s.settings(args.ServiceArgs).Browser()
	if own, ok := s.victoria.UserLevel(strconv.FormatUint(args.Principal.UserID, 10)); ok && own < lowest {
		lowest = own // full logging for this user
	}
	resp := logsservice.IngestResponse{Common: features.OK()}
	for _, r := range args.Records {
		level, err := logging.ParseLevel(r.Level)
		if err != nil || level < lowest {
			continue
		}
		line, err := json.Marshal(s.browserLine(args.Principal, r))
		if err != nil {
			continue
		}
		s.victoria.Enqueue(line)
		resp.Accepted++
	}
	return resp
}

func (s *service) browserLine(p features.Principal, r logsservice.BrowserRecord) map[string]any {
	now := time.Now()
	at := r.Time
	if at.IsZero() || at.After(now.Add(time.Minute)) {
		at = now
	}
	line := map[string]any{
		"_time": at.UTC().Format(time.RFC3339Nano), "level": r.Level, "_msg": clip(r.Message),
		"app": s.victoria.App(), "instance": s.victoria.Instance(), "source": "browser",
		"tenant.id": strconv.FormatUint(p.TenantID, 10),
	}
	if p.UserID != 0 {
		line["user.id"] = strconv.FormatUint(p.UserID, 10)
	}
	for k, v := range map[string]string{
		"domain": r.Domain, "code": r.Code, "trace": r.Trace, "span": r.Span, "hint": r.Hint, "public": r.Public, "url": r.URL,
	} {
		if v != "" {
			line[k] = clip(v)
		}
	}
	if len(r.Tags) > 0 {
		line["tags"] = r.Tags
	}
	for k, v := range r.Attrs {
		line["attr."+k] = v
	}
	if e := r.Error; e != nil {
		line["error"] = clip(e.Name + ": " + e.Message)
		if e.Stack != "" {
			line["error.stack"] = clip(e.Stack)
		}
		cause := e.Cause
		for cause != nil && cause.Cause != nil {
			cause = cause.Cause
		}
		if cause != nil {
			line["error.cause"] = clip(cause.Message)
		}
	}
	return line
}

func clip(s string) string {
	if len(s) > maxText {
		return s[:maxText] + "…"
	}
	return s
}
