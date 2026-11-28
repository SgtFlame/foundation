// Package caldavtest is a CalDAV server in memory for calendar.caldav's
// tests (forge 28l), written from RFC 4791 (CalDAV), RFC 6578 (WebDAV
// sync) and RFC 6764 (/.well-known/caldav) in place of recordings of real
// accounts (question 28.12): discovery through a principal and a calendar
// home, calendar-query with time ranges, calendar-multiget,
// sync-collection with 404 members, truncation (507) and expired tokens,
// free-busy-query, and conditional PUT and DELETE. Its options make it
// act like a Nextcloud (everything) or an iCloud (no sync-collection, no
// free-busy-query).
package caldavtest

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
)

// Options shape the server.
type Options struct {
	User, Password string
	// NoSync leaves out sync-collection (iCloud-like).
	NoSync bool
	// NoFreeBusy refuses free-busy-query (501).
	NoFreeBusy bool
	// SyncPage truncates a sync-collection answer after this many members
	// with a 507 for the collection (0: never).
	SyncPage int
}

// Server is the CalDAV server.
type Server struct {
	o  Options
	mu sync.Mutex
	// cals by id: personal (VEVENT, writable), team (VEVENT, writable),
	// tasks (VTODO only), holidays (read only).
	cals map[string]*cal
	log  []change // every change, the sync token its length
	// expired: tokens up to this one are refused (-1: none).
	expired int
	// lose makes the next n PUTs succeed and then answer 502, as an
	// answer lost on the way.
	lose int
	// Requests counts requests by method.
	Requests map[string]int
}

type cal struct {
	id, name, color string
	comps           string
	readOnly        bool
	res             map[string]*resource // by name without .ics
}

type resource struct {
	data string
	etag int
}

type change struct {
	cal, name string
}

// New makes a server with the user's calendars.
func New(o Options) *Server {
	s := &Server{o: o, cals: map[string]*cal{}, Requests: map[string]int{}, expired: -1}
	s.cals["personal"] = &cal{id: "personal", name: "Personal", color: "#3f51b5ff", comps: "VEVENT", res: map[string]*resource{}}
	s.cals["team"] = &cal{id: "team", name: "Team", color: "#009688ff", comps: "VEVENT", res: map[string]*resource{}}
	s.cals["tasks"] = &cal{id: "tasks", name: "Tasks", comps: "VTODO", res: map[string]*resource{}}
	s.cals["holidays"] = &cal{id: "holidays", name: "Holidays", comps: "VEVENT", readOnly: true, res: map[string]*resource{}}
	return s
}

// Paths.
const (
	Principal = "/dav/principals/ana/"
	Home      = "/dav/calendars/ana/"
)

// Expire makes every sync token given so far too old.
func (s *Server) Expire() {
	s.mu.Lock()
	s.expired = len(s.log)
	s.mu.Unlock()
}

// LoseNextAnswers makes the next n PUTs take effect and answer 502.
func (s *Server) LoseNextAnswers(n int) {
	s.mu.Lock()
	s.lose = n
	s.mu.Unlock()
}

// Put stores a resource as someone else would (no conditions).
func (s *Server) Put(calID, name, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cals[calID]
	r := c.res[name]
	if r == nil {
		r = &resource{}
		c.res[name] = r
	}
	r.data, r.etag = data, r.etag+1
	s.log = append(s.log, change{calID, name})
}

// Remove deletes a resource as someone else would.
func (s *Server) Remove(calID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cals[calID].res, name)
	s.log = append(s.log, change{calID, name})
}

// Data is a resource's text, "" when there is none.
func (s *Server) Data(calID, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.cals[calID].res[name]; r != nil {
		return r.data
	}
	return ""
}

// Count is how many resources a calendar holds.
func (s *Server) Count(calID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cals[calID].res)
}

func etag(n int) string { return fmt.Sprintf(`"e%d"`, n) }

func token(n int) string { return "http://caldav.example.test/ns/sync/" + strconv.Itoa(n) }

const xmlHead = `<?xml version="1.0" encoding="utf-8"?>` + "\n"

func multistatus(body string, syncToken string) string {
	t := ""
	if syncToken != "" {
		t = "<d:sync-token>" + syncToken + "</d:sync-token>"
	}
	return xmlHead + `<d:multistatus xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav" xmlns:ical="http://apple.com/ns/ical/">` + body + t + `</d:multistatus>`
}

func propOK(href, props string) string {
	return "<d:response><d:href>" + href + "</d:href><d:propstat><d:prop>" + props + "</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>"
}

func status(href string, code int) string {
	return fmt.Sprintf("<d:response><d:href>%s</d:href><d:status>HTTP/1.1 %d %s</d:status></d:response>", href, code, http.StatusText(code))
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

var (
	reTimeRange = regexp.MustCompile(`time-range start="([0-9TZ]+)" end="([0-9TZ]+)"`)
	reHref      = regexp.MustCompile(`<[A-Za-z]*:?href>([^<]+)</[A-Za-z]*:?href>`)
	reToken     = regexp.MustCompile(`<[A-Za-z]*:?sync-token>([^<]*)</[A-Za-z]*:?sync-token>`)
)

// ServeHTTP answers one request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests[r.Method]++
	if r.URL.Path == "/.well-known/caldav" {
		http.Redirect(w, r, "/dav/", http.StatusMovedPermanently)
		return
	}
	if s.o.User != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != s.o.User || p != s.o.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="caldav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	p := r.URL.Path
	switch {
	case r.Method == "PROPFIND" && (p == "/dav/" || p == Principal):
		s.write(w, http.StatusMultiStatus, propOK(p, `<d:current-user-principal><d:href>`+Principal+`</d:href></d:current-user-principal><cal:calendar-home-set><d:href>`+Home+`</d:href></cal:calendar-home-set><d:resourcetype><d:collection/></d:resourcetype>`))
	case r.Method == "PROPFIND" && p == Home:
		if r.Header.Get("Depth") != "1" {
			s.write(w, http.StatusMultiStatus, propOK(Home, "<d:resourcetype><d:collection/></d:resourcetype>"))
			return
		}
		out := propOK(Home, "<d:resourcetype><d:collection/></d:resourcetype>")
		ids := make([]string, 0, len(s.cals))
		for id := range s.cals {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			c := s.cals[id]
			priv := "<d:privilege><d:read/></d:privilege>"
			if !c.readOnly {
				priv += "<d:privilege><d:write/></d:privilege>"
			}
			reports := "<d:supported-report><d:report><cal:calendar-query/></d:report></d:supported-report><d:supported-report><d:report><cal:calendar-multiget/></d:report></d:supported-report>"
			if !s.o.NoSync {
				reports += "<d:supported-report><d:report><d:sync-collection/></d:report></d:supported-report>"
			}
			props := "<d:resourcetype><d:collection/><cal:calendar/></d:resourcetype><d:displayname>" + c.name + "</d:displayname>" +
				"<cal:supported-calendar-component-set><cal:comp name=\"" + c.comps + "\"/></cal:supported-calendar-component-set>" +
				"<d:current-user-privilege-set>" + priv + "</d:current-user-privilege-set><d:supported-report-set>" + reports + "</d:supported-report-set>" +
				"<cal:calendar-timezone>BEGIN:VCALENDAR&#13;\nBEGIN:VTIMEZONE&#13;\nTZID:Europe/London&#13;\nEND:VTIMEZONE&#13;\nEND:VCALENDAR&#13;\n</cal:calendar-timezone>"
			if c.color != "" {
				props += "<ical:calendar-color>" + c.color + "</ical:calendar-color>"
			}
			out += propOK(Home+id+"/", props)
		}
		s.write(w, http.StatusMultiStatus, out)
	default:
		rest, ok := strings.CutPrefix(p, Home)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calID, name, _ := strings.Cut(rest, "/")
		c := s.cals[calID]
		if c == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		name, _ = url.PathUnescape(strings.TrimSuffix(name, ".ics"))
		if name == "" {
			s.collection(w, r, c, body)
			return
		}
		s.resource(w, r, c, name, body)
	}
}

func (s *Server) write(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(code)
	_, _ = io.WriteString(w, multistatus(body, ""))
}

func (s *Server) href(c *cal, name string) string {
	return Home + c.id + "/" + url.PathEscape(name) + ".ics"
}

func (s *Server) collection(w http.ResponseWriter, r *http.Request, c *cal, body string) {
	switch {
	case r.Method == "PROPFIND":
		props := ""
		if !s.o.NoSync && strings.Contains(body, "sync-token") {
			props = "<d:sync-token>" + token(len(s.log)) + "</d:sync-token>"
		}
		s.write(w, http.StatusMultiStatus, propOK(Home+c.id+"/", props))
	case r.Method == "REPORT" && strings.Contains(body, "calendar-query"):
		m := reTimeRange.FindStringSubmatch(body)
		var win when.Window
		if m != nil {
			f, _ := time.Parse("20060102T150405Z", m[1])
			t, _ := time.Parse("20060102T150405Z", m[2])
			win = when.Window{From: f, To: t}
		}
		data := strings.Contains(body, "calendar-data")
		out := ""
		for _, name := range sortedNames(c) {
			res := c.res[name]
			p, err := ical.Parse([]byte(res.data))
			if err != nil {
				continue
			}
			hit := m == nil
			for _, ev := range p.Events {
				hit = hit || providers.Overlaps(ev, win)
			}
			if !hit {
				continue
			}
			props := "<d:getetag>" + escape(etag(res.etag)) + "</d:getetag>"
			if data {
				props += "<cal:calendar-data>" + escape(res.data) + "</cal:calendar-data>"
			}
			out += propOK(s.href(c, name), props)
		}
		s.write(w, http.StatusMultiStatus, out)
	case r.Method == "REPORT" && strings.Contains(body, "calendar-multiget"):
		out := ""
		for _, m := range reHref.FindAllStringSubmatch(body, -1) {
			href := m[1]
			name, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(href, Home+c.id+"/"), ".ics"))
			res := c.res[name]
			if res == nil {
				out += status(href, http.StatusNotFound)
				continue
			}
			out += propOK(href, "<d:getetag>"+escape(etag(res.etag))+"</d:getetag><cal:calendar-data>"+escape(res.data)+"</cal:calendar-data>")
		}
		s.write(w, http.StatusMultiStatus, out)
	case r.Method == "REPORT" && strings.Contains(body, "sync-collection"):
		if s.o.NoSync {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		m := reToken.FindStringSubmatch(body)
		from := 0
		if m != nil && m[1] != "" {
			n, err := strconv.Atoi(strings.TrimPrefix(m[1], token(0)[:len(token(0))-1]))
			if err != nil || n <= s.expired || n > len(s.log) {
				w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, xmlHead+`<d:error xmlns:d="DAV:"><d:valid-sync-token/></d:error>`)
				return
			}
			from = n
		}
		seen := map[string]bool{}
		var names []string
		end := len(s.log)
		for i := from; i < len(s.log); i++ {
			ch := s.log[i]
			if ch.cal != c.id || seen[ch.name] {
				continue
			}
			if s.o.SyncPage > 0 && len(names) == s.o.SyncPage {
				end = i
				break
			}
			seen[ch.name] = true
			names = append(names, ch.name)
		}
		out := ""
		for _, name := range names {
			if res := c.res[name]; res != nil {
				out += propOK(s.href(c, name), "<d:getetag>"+escape(etag(res.etag))+"</d:getetag>")
			} else {
				out += status(s.href(c, name), http.StatusNotFound)
			}
		}
		if end < len(s.log) {
			out += status(Home+c.id+"/", http.StatusInsufficientStorage)
		}
		w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, multistatus(out, token(end)))
	case r.Method == "REPORT" && strings.Contains(body, "free-busy-query"):
		if s.o.NoFreeBusy {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		m := reTimeRange.FindStringSubmatch(body)
		f, _ := time.Parse("20060102T150405Z", m[1])
		t, _ := time.Parse("20060102T150405Z", m[2])
		win := when.Window{From: f, To: t}
		var evs []calendar.Event
		for _, name := range sortedNames(c) {
			if p, err := ical.Parse([]byte(c.res[name].data)); err == nil {
				evs = append(evs, p.Events...)
			}
		}
		var b strings.Builder
		b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//caldavtest//EN\r\nBEGIN:VFREEBUSY\r\nDTSTAMP:20261128T000000Z\r\n")
		b.WriteString("DTSTART:" + m[1] + "\r\nDTEND:" + m[2] + "\r\n")
		for _, busy := range providers.Busy(evs, "", win) {
			bs, _ := time.Parse(time.RFC3339, busy.Start)
			be, _ := time.Parse(time.RFC3339, busy.End)
			b.WriteString("FREEBUSY;FBTYPE=BUSY:" + bs.UTC().Format("20060102T150405Z") + "/" + be.UTC().Format("20060102T150405Z") + "\r\n")
		}
		b.WriteString("END:VFREEBUSY\r\nEND:VCALENDAR\r\n")
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = io.WriteString(w, b.String())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sortedNames(c *cal) []string {
	names := make([]string, 0, len(c.res))
	for n := range c.res {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (s *Server) resource(w http.ResponseWriter, r *http.Request, c *cal, name, body string) {
	res := c.res[name]
	switch r.Method {
	case http.MethodGet:
		if res == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("ETag", etag(res.etag))
		_, _ = io.WriteString(w, res.data)
	case http.MethodPut:
		if c.readOnly {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if inm := r.Header.Get("If-None-Match"); inm == "*" && res != nil {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && (res == nil || im != etag(res.etag)) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		// RFC 4791 4.1: a calendar object resource has no METHOD, and one
		// UID; valid-calendar-object-resource otherwise.
		p, err := ical.Parse([]byte(body))
		if err != nil || strings.Contains(body, "\nMETHOD:") || len(p.Events) == 0 {
			w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, xmlHead+`<d:error xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav"><cal:valid-calendar-object-resource/></d:error>`)
			return
		}
		for _, ev := range p.Events[1:] {
			if ev.ICalUID != p.Events[0].ICalUID {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		code := http.StatusNoContent
		if res == nil {
			res = &resource{}
			c.res[name] = res
			code = http.StatusCreated
		}
		res.data, res.etag = body, res.etag+1
		s.log = append(s.log, change{c.id, name})
		if s.lose > 0 {
			s.lose--
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("ETag", etag(res.etag))
		w.WriteHeader(code)
	case http.MethodDelete:
		if res == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && im != etag(res.etag) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		delete(c.res, name)
		s.log = append(s.log, change{c.id, name})
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
