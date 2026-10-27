package impl

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// fakeVictoria answers the query endpoints and records what it was sent.
func fakeVictoria(t *testing.T) (*httptest.Server, *[]url.Values, *[]string) {
	t.Helper()
	var calls []url.Values
	var ingested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/insert/jsonline" {
			body, _ := io.ReadAll(r.Body)
			ingested = append(ingested, strings.Split(strings.TrimSpace(string(body)), "\n")...)
			return
		}
		_ = r.ParseForm()
		calls = append(calls, r.PostForm)
		switch r.URL.Path {
		case "/select/logsql/query":
			if strings.Contains(r.PostForm.Get("query"), "sortt") {
				http.Error(w, "cannot parse `query` arg [level:error | sortt]: unexpected pipe name \"sortt\"; context: [level:error | sortt]", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"_time":"2026-10-23T08:00:00Z","_msg":"older"}`+"\n"+`{"_time":"2026-10-23T09:00:00Z","_msg":"newer"}`+"\n"+`{"_time":"2026-10-23T07:00:00Z","_msg":"oldest"}`+"\n")
		case "/select/logsql/field_values":
			_, _ = io.WriteString(w, `{"values":[{"value":"error","hits":3}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &ingested
}

func newTestService(t *testing.T, target string) (*service, *logging.Victoria) {
	t.Helper()
	v := logging.NewVictoria(logging.VictoriaOptions{App: "foundation", Instance: "test", FlushEvery: 10 * time.Millisecond, Errors: io.Discard})
	t.Cleanup(func() { v.Close(context.Background()) })
	v.SetTarget(logging.Target{URL: target})
	return newService(WithVictoria(v)), v
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func TestQuerySortsAndLimits(t *testing.T) {
	srv, calls, _ := fakeVictoria(t)
	s, _ := newTestService(t, srv.URL)

	resp := s.Query(logsservice.QueryArgs{ServiceArgs: args(), Query: "level:error", Limit: 2})
	if resp.Common.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Common.Code, resp.Common.Message)
	}
	if len(resp.Rows) != 2 || resp.Rows[0]["_msg"] != "newer" || resp.Rows[1]["_msg"] != "older" || !resp.Limited {
		t.Errorf("rows %v limited %v; want newer, older and limited", resp.Rows, resp.Limited)
	}
	sent := (*calls)[0]
	if sent.Get("limit") != "3" || sent.Get("query") != "level:error" || sent.Get("start") == "" || sent.Get("end") == "" {
		t.Errorf("sent %v", sent)
	}
	// allow-all authz may read every tenant's logs, so nothing is scoped yet
	if sent.Has("extra_filters") {
		t.Errorf("scoped to %q", sent.Get("extra_filters"))
	}
}

func TestQueryParseError(t *testing.T) {
	srv, _, _ := fakeVictoria(t)
	s, _ := newTestService(t, srv.URL)

	resp := s.Query(logsservice.QueryArgs{ServiceArgs: args(), Query: "level:error | sortt"})
	if resp.Common.Code != http.StatusBadRequest || resp.Common.ErrorCode != "bad_query" {
		t.Fatalf("got %d %q", resp.Common.Code, resp.Common.ErrorCode)
	}
	if qe := resp.QueryError; qe == nil || qe.Offset != len("level:error | sortt") || qe.Message != `unexpected pipe name "sortt"` {
		t.Errorf("query error %+v", resp.QueryError)
	}
}

func TestNotConfigured(t *testing.T) {
	s, _ := newTestService(t, "")
	resp := s.FieldValues(logsservice.FieldValuesArgs{ServiceArgs: args(), Field: "level"})
	if resp.Common.Code != http.StatusServiceUnavailable || resp.Common.ErrorCode != "logs_not_configured" {
		t.Errorf("got %d %q", resp.Common.Code, resp.Common.ErrorCode)
	}
	if got := s.Ingest(logsservice.IngestArgs{ServiceArgs: args(), Records: []logsservice.BrowserRecord{{Level: "error"}}}); got.Accepted != 0 {
		t.Errorf("accepted %d without storage", got.Accepted)
	}
}

func TestRangeLimits(t *testing.T) {
	srv, _, _ := fakeVictoria(t)
	s, _ := newTestService(t, srv.URL)
	end := time.Now()
	resp := s.Query(logsservice.QueryArgs{ServiceArgs: args(), Range: logsservice.Range{Start: end.Add(-40 * 24 * time.Hour), End: end}})
	if resp.Common.ErrorCode != "range_too_long" {
		t.Errorf("got %q", resp.Common.ErrorCode)
	}
}

func TestIngestUsesThePrincipal(t *testing.T) {
	srv, _, ingested := fakeVictoria(t)
	s, v := newTestService(t, srv.URL)

	resp := s.Ingest(logsservice.IngestArgs{ServiceArgs: args(), Records: []logsservice.BrowserRecord{
		{Level: "info", Message: "below warn"},
		{Level: "error", Message: "click failed", Domain: "kb", Trace: "tr1", Attrs: map[string]any{"user.id": "spoofed", "volumeId": "42"},
			Error: &logsservice.BrowserError{Name: "ServiceError", Message: "Not Found", Cause: &logsservice.BrowserError{Message: "inner"}}},
	}})
	if resp.Accepted != 1 {
		t.Fatalf("accepted %d, want 1", resp.Accepted)
	}
	v.Close(context.Background())
	if len(*ingested) != 1 {
		t.Fatalf("stored %d lines", len(*ingested))
	}
	var line map[string]any
	if err := json.Unmarshal([]byte((*ingested)[0]), &line); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"_msg": "click failed", "level": "error", "source": "browser", "app": "foundation", "instance": "test",
		"tenant.id": "1", "user.id": "1", "trace": "tr1", "domain": "kb", "attr.volumeId": "42", "attr.user.id": "spoofed",
		"error": "ServiceError: Not Found", "error.cause": "inner"}
	for k, val := range want {
		if line[k] != val {
			t.Errorf("%s = %v, want %v", k, line[k], val)
		}
	}
}
