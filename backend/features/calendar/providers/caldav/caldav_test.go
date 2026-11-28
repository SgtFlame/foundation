package caldav_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/contracttest"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers/caldav"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers/caldav/caldavtest"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

func TestMain(m *testing.M) {
	if ical.ChildRequested() {
		ical.ServeChild()
	}
	os.Exit(m.Run())
}

func ctxFor(op string) adapter.Context {
	return adapter.Context{Context: context.Background(), Contract: calendar.EventsRef, Operation: op, Log: logging.In("caldav-test"),
		Args: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{TenantID: 1}}}
}

const self = "ana@example.test"

func open(t *testing.T, srv *httptest.Server, o caldav.Options, st map[string]any) adapter.Session {
	t.Helper()
	if o.Read == nil {
		o.Read = providers.InProcess
	}
	if st == nil {
		st = map[string]any{"url": srv.URL + "/dav/", "email": self, "username": "ana", "password": "app-pass"}
	}
	raw, _ := json.Marshal(st)
	s, err := caldav.Impl(o).Open(ctxFor(""), adapter.Connection{ID: "c1", Type: caldav.TypeID, Tenant: 1, Settings: raw})
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

func server(o caldavtest.Options) (*caldavtest.Server, *httptest.Server) {
	o.User, o.Password = "ana", "app-pass"
	s := caldavtest.New(o)
	return s, httptest.NewServer(s)
}

// A Nextcloud-like server: sync-collection, free-busy-query.
func TestContractNextcloudLike(t *testing.T) {
	s, srv := server(caldavtest.Options{})
	defer srv.Close()
	contracttest.Run(t, caller(open(t, srv, caldav.Options{}, nil)), contracttest.Options{Self: self, Expire: func() error { s.Expire(); return nil }})
}

// An iCloud-like server: no sync-collection (deltas by etag in the
// window), no free-busy-query (computed).
func TestContractICloudLike(t *testing.T) {
	_, srv := server(caldavtest.Options{NoSync: true, NoFreeBusy: true})
	defer srv.Close()
	contracttest.Run(t, caller(open(t, srv, caldav.Options{}, nil)), contracttest.Options{Self: self})
}

type listed struct {
	Events  []calendar.Event `json:"events"`
	Removed []string         `json:"removed"`
	Cursor  string           `json:"cursor"`
}

func list(t *testing.T, call contracttest.Caller, args map[string]any) listed {
	t.Helper()
	raw, err := call("event.list", args)
	var l listed
	if err != nil || json.Unmarshal(raw, &l) != nil {
		t.Fatalf("event.list %v: %v %s", args, err, raw)
	}
	return l
}

var week = map[string]string{"from": "2026-11-30T00:00:00Z", "to": "2026-12-07T00:00:00Z"}

func meeting(title, day string) map[string]any {
	return map[string]any{"title": title, "start": map[string]string{"local": day + "T10:00:00", "zone": "Europe/London"}, "end": map[string]string{"local": day + "T11:00:00", "zone": "Europe/London"}}
}

func TestDiscoveryAndCalendars(t *testing.T) {
	_, srv := server(caldavtest.Options{})
	defer srv.Close()
	// From /.well-known/caldav, redirected (RFC 6764).
	call := caller(open(t, srv, caldav.Options{}, map[string]any{"url": srv.URL + "/.well-known/caldav", "username": self, "password": "app-pass"}))
	if _, err := call("check", map[string]any{}); connectors.AsError(err).Code != connectors.CodeAuth {
		t.Fatalf("a wrong user = %v, want auth", err)
	}
	call = caller(open(t, srv, caldav.Options{}, map[string]any{"url": srv.URL + "/.well-known/caldav", "email": self, "username": "ana", "password": "app-pass"}))
	raw, err := call("calendar.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var cals struct{ Calendars []calendar.Calendar }
	_ = json.Unmarshal(raw, &cals)
	got := map[string]calendar.Calendar{}
	for _, c := range cals.Calendars {
		got[c.ID] = c
	}
	if len(cals.Calendars) != 3 || got["tasks"].ID != "" {
		t.Fatalf("calendars = %+v, want three without the task list", cals.Calendars)
	}
	if h := got["holidays"]; h.Access != "reader" || h.Primary {
		t.Fatalf("holidays = %+v", h)
	}
	if p := got["personal"]; !p.Primary || p.Access != "owner" || p.Color != "#3f51b5" || p.Zone != "Europe/London" || p.Name != "Personal" {
		t.Fatalf("personal = %+v", p)
	}
	if _, err := caldav.New("c", caldav.Settings{Username: "ana"}, caldav.Options{}); connectors.AsError(err).Code != connectors.CodeInvalid {
		t.Fatalf("no URL and no email = %v", err)
	}
}

// A create whose answer is lost is tried again under the same key and
// makes one resource (If-None-Match: *).
func TestCreateRetriedAfterALostAnswer(t *testing.T) {
	s, srv := server(caldavtest.Options{})
	defer srv.Close()
	call := caller(open(t, srv, caldav.Options{}, nil))
	s.LoseNextAnswers(1)
	args := map[string]any{"calendar": "personal", "key": "rec 42/x", "event": meeting("Lost answer", "2026-12-02")}
	if _, err := call("event.create", args); connectors.AsError(err).Code != connectors.CodeUnavailable || !connectors.AsError(err).Retryable() {
		t.Fatalf("a lost answer = %v, want a retryable unavailable", err)
	}
	raw, err := call("event.create", args)
	if err != nil {
		t.Fatal(err)
	}
	var ev calendar.Event
	_ = json.Unmarshal(raw, &ev)
	if s.Count("personal") != 1 || ev.ID != "rec-42-x" || ev.ICalUID != "rec 42/x@axiom" || ev.Etag == "" {
		t.Fatalf("after the retry: %d resources, %+v", s.Count("personal"), ev)
	}
	if strings.Contains(s.Data("personal", "rec-42-x"), "METHOD:") {
		t.Fatal("a stored resource carries METHOD")
	}
}

// Changes made at the server come through the cursor, across a truncated
// sync-collection; an expired token is cursor_expired.
func TestOutsideChangesAndTruncatedSync(t *testing.T) {
	s, srv := server(caldavtest.Options{SyncPage: 1})
	defer srv.Close()
	call := caller(open(t, srv, caldav.Options{}, nil))
	first := list(t, call, map[string]any{"calendar": "team", "window": week})
	for i, day := range []string{"2026-12-01", "2026-12-02", "2026-12-03"} {
		text, _ := ical.Compose("", []calendar.Event{{ICalUID: "out-" + day, Title: "Outside " + string(rune('A'+i)), Start: zoned(day + "T09:00:00"), End: zoned(day + "T09:30:00")}}, ical.ComposeOptions{Resource: true})
		s.Put("team", "out-"+day, text)
	}
	d := list(t, call, map[string]any{"calendar": "team", "cursor": first.Cursor})
	if len(d.Events) != 3 {
		t.Fatalf("three outside events over a sync paged by one = %+v", d.Events)
	}
	s.Remove("team", "out-2026-12-02")
	d2 := list(t, call, map[string]any{"calendar": "team", "cursor": d.Cursor})
	if len(d2.Events) != 0 || len(d2.Removed) != 1 || d2.Removed[0] != "out-2026-12-02" {
		t.Fatalf("after an outside delete = %+v", d2)
	}
	s.Expire()
	if _, err := call("event.list", map[string]any{"calendar": "team", "cursor": d2.Cursor}); !strings.Contains(err.Error(), calendar.CodeCursorExpired) {
		t.Fatalf("an expired token = %v", err)
	}
}

func zoned(local string) when.Zoned { return when.Zoned{Local: local, Zone: "Europe/London"} }

// caldavCursor is the cursor a sync token makes (the type's own form).
func caldavCursor(token string) string {
	raw, _ := json.Marshal(map[string]string{"t": token})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// A series and its exceptions are one resource: an exception made, listed
// with its series, and deleted as an EXDATE.
func TestSeriesExceptions(t *testing.T) {
	s, srv := server(caldavtest.Options{NoSync: true})
	defer srv.Close()
	call := caller(open(t, srv, caldav.Options{}, nil))
	ser := meeting("Weekly", "2026-11-30")
	ser["recurrence"] = map[string]any{"rrule": "FREQ=WEEKLY;BYDAY=MO"}
	raw, err := call("event.create", map[string]any{"calendar": "personal", "key": "weekly", "event": ser})
	if err != nil {
		t.Fatal(err)
	}
	var master calendar.Event
	_ = json.Unmarshal(raw, &master)
	moved := meeting("Weekly (moved)", "2026-12-08")
	moved["seriesId"], moved["recurrenceId"] = master.ID, "2026-12-07T10:00:00Z"
	raw, err = call("event.create", map[string]any{"calendar": "personal", "key": "weekly-x", "event": moved})
	if err != nil {
		t.Fatal(err)
	}
	var ex calendar.Event
	_ = json.Unmarshal(raw, &ex)
	if ex.SeriesID != "weekly" || !strings.HasPrefix(ex.ID, "weekly_") || s.Count("personal") != 1 {
		t.Fatalf("the exception = %+v in %d resources", ex, s.Count("personal"))
	}
	l := list(t, call, map[string]any{"calendar": "personal", "window": map[string]string{"from": "2026-12-07T00:00:00Z", "to": "2026-12-14T00:00:00Z"}})
	if len(l.Events) != 2 {
		t.Fatalf("the series' week lists %+v", l.Events)
	}
	var fb struct{ FreeBusy []calendar.FreeBusy }
	raw, _ = call("freeBusy", map[string]any{"calendars": []string{"personal"}, "window": map[string]string{"from": "2026-12-07T00:00:00Z", "to": "2026-12-09T00:00:00Z"}})
	_ = json.Unmarshal(raw, &fb)
	if len(fb.FreeBusy) != 1 || len(fb.FreeBusy[0].Busy) != 1 || !strings.HasPrefix(fb.FreeBusy[0].Busy[0].Start, "2026-12-08") {
		t.Fatalf("free/busy with a moved occurrence = %+v", fb)
	}
	raw, err = call("event.delete", map[string]any{"calendar": "personal", "id": ex.ID})
	if err != nil || !strings.Contains(string(raw), "true") {
		t.Fatalf("deleting the exception = %s %v", raw, err)
	}
	if d := s.Data("personal", "weekly"); !strings.Contains(d, "EXDATE") || strings.Contains(d, "RECURRENCE-ID") {
		t.Fatalf("the series after deleting its exception:\n%s", d)
	}
}

// Watch polls and raises a notice for an outside change.
func TestWatchPolls(t *testing.T) {
	for _, o := range []caldavtest.Options{{}, {NoSync: true}} {
		s, srv := server(o)
		sess := open(t, srv, caldav.Options{Poll: 20 * time.Millisecond, Now: func() time.Time { return time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC) }}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		var mu sync.Mutex
		var got []adapter.Event
		done := make(chan struct{})
		go func() {
			c := ctxFor("")
			c.Context = ctx
			_ = sess.(adapter.Watcher).Watch(c, nil, func(e adapter.Event) error {
				mu.Lock()
				got = append(got, e)
				mu.Unlock()
				return nil
			})
			close(done)
		}()
		time.Sleep(100 * time.Millisecond)
		ev := meeting("Watched", "2026-12-04")
		if _, err := caller(sess)("event.create", map[string]any{"calendar": "team", "key": "watched", "event": ev}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			mu.Lock()
			n := len(got)
			mu.Unlock()
			if n > 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		<-done
		srv.Close()
		mu.Lock()
		if len(got) == 0 || got[0].Name != "eventChanged" || !strings.Contains(string(got[0].Payload), `"event":"watched"`) || len(got[len(got)-1].Cursor) == 0 {
			t.Fatalf("NoSync=%v: notices %+v", o.NoSync, got)
		}
		mu.Unlock()
		_ = s
	}
}

// A multistatus in RFC 6578's own form (section 6.3: a default DAV:
// namespace, a 404 member, a new token), as servers write it.
func TestRFC6578Answer(t *testing.T) {
	const discovery = `<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:response><D:href>/home/</D:href><D:propstat><D:prop><C:calendar-home-set><D:href>/home/</D:href></C:calendar-home-set></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`
	const home = `<?xml version="1.0" encoding="utf-8" ?>
<multistatus xmlns="DAV:"><response><href>/home/</href><propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>
<response><href>/home/cal/</href><propstat><prop><resourcetype><collection/><calendar xmlns="urn:ietf:params:xml:ns:caldav"/></resourcetype><displayname>Calendar</displayname><supported-report-set><supported-report><report><sync-collection/></report></supported-report></supported-report-set></prop><status>HTTP/1.1 200 OK</status></propstat>
<propstat><prop><calendar-color xmlns="http://apple.com/ns/ical/"/></prop><status>HTTP/1.1 404 Not Found</status></propstat></response></multistatus>`
	const sync = `<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:"><D:response><D:href>/home/cal/test.ics</D:href><D:propstat><D:prop><D:getetag>"00001-abcd2"</D:getetag></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>
<D:response><D:href>/home/cal/vcard.vcf</D:href><D:status>HTTP/1.1 404 Not Found</D:status></D:response><D:sync-token>http://example.com/ns/sync/1238</D:sync-token></D:multistatus>`
	const data = `<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:response><D:href>/home/cal/test.ics</D:href><D:propstat><D:prop><D:getetag>"00001-abcd2"</D:getetag><C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Example Corp.//CalDAV Client//EN
BEGIN:VEVENT
UID:74855313FA803DA593CD579A@example.com
DTSTAMP:20060206T001102Z
DTSTART:20261202T170000Z
DURATION:PT1H
SUMMARY:Event #1
END:VEVENT
END:VCALENDAR
</C:calendar-data></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`
	answers := map[string]string{"PROPFIND /home/ 0": discovery, "PROPFIND /home/ 1": home, "REPORT sync": sync, "REPORT multiget": data}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, r.Body)
		key := r.Method + " " + r.URL.Path + " " + r.Header.Get("Depth")
		switch {
		case r.Method == "REPORT" && strings.Contains(buf.String(), "sync-collection"):
			key = "REPORT sync"
		case r.Method == "REPORT" && strings.Contains(buf.String(), "calendar-multiget"):
			key = "REPORT multiget"
		}
		a, ok := answers[key]
		if !ok {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(a))
	}))
	defer srv.Close()
	call := caller(open(t, srv, caldav.Options{}, map[string]any{"url": srv.URL + "/home/", "username": "u", "password": "p"}))
	raw, err := call("calendar.list", map[string]any{})
	if err != nil || !strings.Contains(string(raw), `"id":"cal"`) {
		t.Fatalf("calendar.list = %s %v", raw, err)
	}
	cur := caldavCursor("http://example.com/ns/sync/1234")
	l := list(t, call, map[string]any{"calendar": "cal", "cursor": cur})
	if len(l.Events) != 1 || l.Events[0].ID != "test" || l.Events[0].Etag != `"00001-abcd2"` || len(l.Removed) != 1 || l.Removed[0] != "vcard.vcf" {
		t.Fatalf("the RFC 6578 delta = %+v", l)
	}
}

func TestType(t *testing.T) {
	if ps := connectors.CheckType(caldav.Type(), func(ref string) (connectors.Contract, bool) {
		return calendar.EventsContract(), ref == calendar.EventsRef
	}); len(ps) > 0 {
		t.Fatal(ps)
	}
}
