package logsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// IngestRequest carries browser LogRecords (src/logging/LogRecord.ts). It
// names no user or tenant: those come from the principal.
type IngestRequest struct {
	Records []BrowserRecord `json:"records"`
}

type BrowserRecord struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Domain  string         `json:"domain,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Code    string         `json:"code,omitempty"`
	Trace   string         `json:"trace,omitempty"`
	Span    string         `json:"span,omitempty"`
	Hint    string         `json:"hint,omitempty"`
	Public  string         `json:"public,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Error   *BrowserError  `json:"error,omitempty"`
	URL     string         `json:"url,omitempty"`
}

type BrowserError struct {
	Name    string        `json:"name"`
	Message string        `json:"message"`
	Stack   string        `json:"stack,omitempty"`
	Cause   *BrowserError `json:"cause,omitempty"`
}

func (e *BrowserError) toError() *logsservice.BrowserError {
	if e == nil {
		return nil
	}
	return &logsservice.BrowserError{Name: e.Name, Message: e.Message, Stack: e.Stack, Cause: e.Cause.toError()}
}

// ToArgs converts the request to the service's IngestArgs.
func (r IngestRequest) ToArgs(args features.ServiceArgs) logsservice.IngestArgs {
	out := logsservice.IngestArgs{ServiceArgs: args}
	for _, rec := range r.Records {
		out.Records = append(out.Records, logsservice.BrowserRecord{
			Time: rec.Time, Level: rec.Level, Message: rec.Message, Domain: rec.Domain, Tags: rec.Tags, Code: rec.Code,
			Trace: rec.Trace, Span: rec.Span, Hint: rec.Hint, Public: rec.Public, Attrs: rec.Attrs, Error: rec.Error.toError(), URL: rec.URL,
		})
	}
	return out
}
