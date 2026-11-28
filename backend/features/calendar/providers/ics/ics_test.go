package ics_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/contracttest"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers/ics"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
)

// TestMain serves ical's child, so the hostile feed is refused by a real
// child process of this test binary.
func TestMain(m *testing.M) {
	if ical.ChildRequested() {
		ical.ServeChild()
	}
	os.Exit(m.Run())
}

const holidays = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//Holidays//EN\r\nX-WR-CALNAME:Holidays\r\n" +
	"BEGIN:VEVENT\r\nUID:h-christmas@example.org\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;VALUE=DATE:20261225\r\nDTEND;VALUE=DATE:20261226\r\nSUMMARY:Christmas Day\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:h-standup@example.org\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;TZID=Europe/London:20261102T093000\r\nDTEND;TZID=Europe/London:20261102T094500\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE\r\nSUMMARY:Public stand-up\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// feedServer serves body with an etag, answering 304 to a matching
// If-None-Match, and counts full answers.
type feedServer struct {
	mu        sync.Mutex
	body      string
	etag      string
	full, all int
}

func (f *feedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.all++
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == f.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f.full++
	w.Header().Set("ETag", f.etag)
	w.Header().Set("Content-Type", "text/calendar")
	_, _ = w.Write([]byte(f.body))
}

func (f *feedServer) set(body, etag string) {
	f.mu.Lock()
	f.body, f.etag = body, etag
	f.mu.Unlock()
}

func ctxFor(op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: calendar.EventsRef, Operation: op, Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: 1}}}
}

func open(t *testing.T, url string, read ical.Reader) adapter.Session {
	t.Helper()
	settings, _ := json.Marshal(map[string]any{"url": url, "name": "Holidays", "zone": "Europe/London"})
	s, err := ics.Impl(ics.Options{Read: read}).Open(ctxFor(""), adapter.Connection{ID: "c1", Type: ics.TypeID, Tenant: 1, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func caller(s adapter.Session) contracttest.Caller {
	return func(op string, args any) (json.RawMessage, error) {
		raw, _ := json.Marshal(args)
		return s.Call(ctxFor(op), op, raw)
	}
}

func TestContractReadOnly(t *testing.T) {
	f := &feedServer{body: holidays, etag: `"v1"`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	contracttest.Run(t, caller(open(t, srv.URL, providers.InProcess)), contracttest.Options{ReadOnly: true})
	for _, op := range []string{"event.update", "event.delete", "event.respond"} {
		_, err := caller(open(t, srv.URL, providers.InProcess))(op, map[string]any{"calendar": ics.CalendarID, "id": "x", "set": map[string]any{}, "answer": "accepted"})
		if connectors.AsError(err).Code != connectors.CodeNotSupported {
			t.Fatalf("%s = %v, want notSupported", op, err)
		}
	}
}

type listed struct {
	Events  []calendar.Event `json:"events"`
	Removed []string         `json:"removed"`
	Cursor  string           `json:"cursor"`
}

func TestDeltaSkipsAnUnchangedFeed(t *testing.T) {
	f := &feedServer{body: holidays, etag: `"v1"`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	call := caller(open(t, "webcal://"+strings.TrimPrefix(srv.URL, "http://"), providers.InProcess))
	// webcal:// is https://, which this test server is not: the URL is
	// still taken, and refused only by the fetch.
	if _, err := call("check", map[string]any{}); err == nil {
		t.Fatal("an https fetch of a plain http server worked")
	}
	call = caller(open(t, srv.URL, providers.InProcess))
	var w listed
	raw, err := call("event.list", map[string]any{"calendar": ics.CalendarID, "window": map[string]string{"from": "2026-12-20T00:00:00Z", "to": "2026-12-31T00:00:00Z"}})
	if err != nil || json.Unmarshal(raw, &w) != nil {
		t.Fatal(err, string(raw))
	}
	if len(w.Events) != 2 {
		t.Fatalf("the window lists %+v", w.Events)
	}
	full := f.full
	var d listed
	raw, _ = call("event.list", map[string]any{"calendar": ics.CalendarID, "cursor": w.Cursor})
	_ = json.Unmarshal(raw, &d)
	if len(d.Events) != 0 || len(d.Removed) != 0 || d.Cursor != w.Cursor || f.full != full {
		t.Fatalf("an unchanged feed's delta = %+v after %d full fetches (was %d)", d, f.full, full)
	}
	// The stand-up moves to Tuesdays and Christmas goes: one changed, one
	// removed.
	f.set(strings.Replace(strings.Replace(holidays, "BYDAY=MO,WE", "BYDAY=TU", 1),
		"BEGIN:VEVENT\r\nUID:h-christmas@example.org\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;VALUE=DATE:20261225\r\nDTEND;VALUE=DATE:20261226\r\nSUMMARY:Christmas Day\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\n", "", 1), `"v2"`)
	raw, _ = call("event.list", map[string]any{"calendar": ics.CalendarID, "cursor": w.Cursor})
	_ = json.Unmarshal(raw, &d)
	if len(d.Events) != 1 || d.Events[0].ID != "h-standup@example.org" || len(d.Removed) != 1 || d.Removed[0] != "h-christmas@example.org" {
		t.Fatalf("the delta = %+v", d)
	}
	var fb struct{ FreeBusy []calendar.FreeBusy }
	raw, _ = call("freeBusy", map[string]any{"calendars": []string{ics.CalendarID}, "window": map[string]string{"from": "2026-12-01T00:00:00Z", "to": "2026-12-08T00:00:00Z"}})
	_ = json.Unmarshal(raw, &fb)
	if len(fb.FreeBusy) != 1 || len(fb.FreeBusy[0].Busy) != 1 {
		t.Fatalf("freeBusy over a week of Tuesday stand-ups = %+v", fb)
	}
}

func TestHostileFeedsAreRefused(t *testing.T) {
	f := &feedServer{body: "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nDTSTART:garbage\r\n" + strings.Repeat("X-A:b\r\n BEGIN:", 3), etag: `"bad"`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	// Read by the real child (nil Read).
	_, err := caller(open(t, srv.URL, nil))("check", map[string]any{})
	if ce := connectors.AsError(err); ce.Code != connectors.CodeInvalid || !strings.Contains(ce.Message, "could not be read") {
		t.Fatalf("a malformed feed = %v, want invalid", err)
	}
	f.set("BEGIN:VCALENDAR\r\n"+strings.Repeat("X-PAD:"+strings.Repeat("a", 70)+"\r\n", ics.MaxFeed/70), `"big"`)
	_, err = caller(open(t, srv.URL, nil))("check", map[string]any{})
	if ce := connectors.AsError(err); ce.Code != connectors.CodeInvalid || !strings.Contains(ce.Message, "larger") {
		t.Fatalf("an oversized feed = %v, want invalid", err)
	}
	if _, err := ics.FeedURL("ftp://example.org/x.ics"); connectors.AsError(err).Code != connectors.CodeInvalid {
		t.Fatalf("an ftp URL = %v", err)
	}
}

func TestType(t *testing.T) {
	if ps := connectors.CheckType(ics.Type(), func(ref string) (connectors.Contract, bool) {
		return calendar.EventsContract(), ref == calendar.EventsRef
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}
