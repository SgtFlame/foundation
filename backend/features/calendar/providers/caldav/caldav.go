// Package caldav is the calendar.caldav connector type (forge 28l):
// calendar.events@1 on a CalDAV server (RFC 4791: iCloud, Fastmail,
// Nextcloud and others) with a user name and an app password.
//
//   - Discovery: the server URL (or https://<email domain>/.well-known/caldav)
//     → current-user-principal → calendar-home-set → its calendars.
//   - A calendar's id is its collection's last path segment; an event's id
//     is its resource's name without .ics (an exception's adds _<its
//     recurrence id>, its seriesId the resource's), its etag the
//     resource's.
//   - event.list: calendar-query within the window; with a cursor,
//     sync-collection (RFC 6578) from its sync token, or on a server
//     without it a window query compared by etag.
//   - event.create: PUT with If-None-Match: * to a resource named from
//     our key, so a retry finds what the first made; update, respond and
//     delete with If-Match. A series and its exceptions are one resource.
//   - freeBusy: free-busy-query where the server answers it, else computed
//     from a window query.
//   - Watch: polls sync-collection (or the window query) every pollSeconds.
//
// Every calendar-data the server returns is read by ical's child process.
package caldav

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/calendar/when"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
)

// TypeID is the connector type.
const TypeID = "calendar.caldav"

// SettingsSchema is calendar.caldav's settings.
const SettingsSchema = `{"type":"object","properties":{
  "url":{"type":"string","title":"Server URL","description":"The CalDAV server's address (e.g. https://caldav.fastmail.com/ or a Nextcloud's https://cloud.example.org/remote.php/dav); found from the email's domain through /.well-known/caldav when empty"},
  "email":{"type":"string","title":"Email","description":"The account's email: its own attendee entry when answering invitations"},
  "username":{"type":"string","title":"User name","description":"The account's user name, often the email"},
  "password":{"type":"secret","title":"App password","description":"An app-specific password made for Axiom in the account's security settings"},
  "pollSeconds":{"type":"integer","title":"Check for changes every (seconds)","minimum":30,"description":"CalDAV has no push the open web can use, so changes are polled; 60 when empty"}},
  "required":["username","password"]}`

// Type is calendar.caldav.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{calendar.EventsRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "CalDAV calendar", Description: "Calendars on a CalDAV server (iCloud, Fastmail, Nextcloud, Radicale and others) with a user name and an app password: implements calendar.events@1 with discovery from the server or the email's domain, changes by sync-collection where the server has it (else by comparing etags in a window), keyed creates and etag-checked writes, answering invitations as the account and free/busy. Changes are polled every minute.", Tags: []string{"calendar", "caldav", "icloud", "fastmail", "nextcloud"}},
	}
}

// Settings are a connection's settings, resolved.
type Settings struct {
	URL         string `json:"url"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PollSeconds int    `json:"pollSeconds"`
}

// Options are for tests: HTTP the client (its redirects are followed by
// the type itself), Read the .ics reader (the child when nil), Poll
// replacing the poll interval, Now the clock.
type Options struct {
	HTTP *http.Client
	Read ical.Reader
	Poll time.Duration
	Now  func() time.Time
}

// Impl is the adapter implementation.
func Impl(o Options) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, providers.Errorf(connectors.CodeInvalid, "the connection's settings are not valid: %v", err)
			}
		}
		return New(conn.ID, st, o)
	}}
}

// New opens a session.
func New(conn string, st Settings, o Options) (*Session, error) {
	start, err := startURL(st)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(start)
	hc := &http.Client{Timeout: providers.DefaultTimeout}
	if o.HTTP != nil {
		c := *o.HTTP
		hc = &c
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	every := o.Poll
	if every == 0 {
		every = time.Duration(max(st.PollSeconds, 30)) * time.Second
		if st.PollSeconds == 0 {
			every = time.Minute
		}
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	self := st.Email
	if self == "" && strings.Contains(st.Username, "@") {
		self = st.Username
	}
	return &Session{conn: conn, start: start, self: self, read: providers.Reader(o.Read), every: every, now: now,
		c: &client{base: &url.URL{Scheme: base.Scheme, Host: base.Host}, user: st.Username, pass: st.Password, hc: hc}}, nil
}

// startURL is where discovery starts.
func startURL(st Settings) (string, error) {
	raw := strings.TrimSpace(st.URL)
	if raw == "" {
		_, domain, ok := strings.Cut(strings.TrimSpace(st.Email), "@")
		if !ok || domain == "" {
			_, domain, ok = strings.Cut(strings.TrimSpace(st.Username), "@")
		}
		if !ok || domain == "" {
			return "", providers.Errorf(connectors.CodeInvalid, "give the server URL, or an email whose domain has /.well-known/caldav")
		}
		raw = "https://" + domain + "/.well-known/caldav"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", providers.Errorf(connectors.CodeInvalid, "the server URL %q is not an http(s) address", raw)
	}
	return u.String(), nil
}

// Session is one connection's account.
type Session struct {
	conn  string
	start string
	self  string
	read  ical.Reader
	every time.Duration
	now   func() time.Time
	c     *client

	mu   sync.Mutex
	home string
	cals map[string]*collection // by id
}

// collection is a calendar on the server.
type collection struct {
	id, href string
	cal      calendar.Calendar
	sync     bool // supports sync-collection
}

func (x *Session) Close() error { return nil }

// discover finds the calendar home.
func (x *Session) discover(ctx adapter.Context) (string, error) {
	x.mu.Lock()
	home := x.home
	x.mu.Unlock()
	if home != "" {
		return home, nil
	}
	a, err := x.c.do(ctx, "discovering the calendars", "PROPFIND", x.start, map[string]string{"Depth": "0"}, propfindPrincipal)
	if err != nil {
		return "", err
	}
	if a.status != http.StatusMultiStatus {
		return "", a.fail("discovering the calendars")
	}
	ms, err := a.multistatus("discovering the calendars")
	if err != nil {
		return "", err
	}
	var p prop
	if len(ms.Responses) > 0 {
		p = ms.Responses[0].found()
	}
	switch {
	case p.CalendarHome != nil && p.CalendarHome.Href != "":
		home = p.CalendarHome.Href
	case p.CurrentPrincipal != nil && p.CurrentPrincipal.Href != "":
		a, err := x.c.do(ctx, "reading the calendar home", "PROPFIND", p.CurrentPrincipal.Href, map[string]string{"Depth": "0"}, propfindHome)
		if err != nil {
			return "", err
		}
		if a.status != http.StatusMultiStatus {
			return "", a.fail("reading the calendar home")
		}
		ms, err := a.multistatus("reading the calendar home")
		if err != nil {
			return "", err
		}
		for _, r := range ms.Responses {
			if h := r.found().CalendarHome; h != nil && h.Href != "" {
				home = h.Href
			}
		}
	case p.ResourceType != nil && p.ResourceType.Calendar != nil:
		// The URL is a calendar itself: its parent is the home.
		u, _ := x.c.resolve(ms.Responses[0].Href)
		home = strings.TrimSuffix(path.Dir(strings.TrimSuffix(u.Path, "/")), "/") + "/"
	}
	if home == "" {
		return "", providers.Errorf(connectors.CodeNotFound, "discovering the calendars: the server names no calendar home for this account")
	}
	if !strings.HasSuffix(home, "/") {
		home += "/"
	}
	x.mu.Lock()
	x.home = home
	x.mu.Unlock()
	return home, nil
}

// lastSegment is a path's last segment, unescaped, without a trailing /.
func lastSegment(href string) string {
	u, err := url.Parse(href)
	p := href
	if err == nil {
		p = u.Path
	}
	p = strings.TrimSuffix(p, "/")
	return path.Base(p)
}

// calendars lists the home's calendar collections.
func (x *Session) calendars(ctx adapter.Context) ([]*collection, error) {
	home, err := x.discover(ctx)
	if err != nil {
		return nil, err
	}
	a, err := x.c.do(ctx, "listing the calendars", "PROPFIND", home, map[string]string{"Depth": "1"}, propfindCalendars)
	if err != nil {
		return nil, err
	}
	if a.status != http.StatusMultiStatus {
		return nil, a.fail("listing the calendars")
	}
	ms, err := a.multistatus("listing the calendars")
	if err != nil {
		return nil, err
	}
	var out []*collection
	for _, r := range ms.Responses {
		p := r.found()
		if p.ResourceType == nil || p.ResourceType.Calendar == nil {
			continue
		}
		if p.Components != nil && len(p.Components.Comp) > 0 {
			events := false
			for _, c := range p.Components.Comp {
				events = events || strings.EqualFold(c.Name, "VEVENT")
			}
			if !events {
				continue // a task list
			}
		}
		id := lastSegment(r.Href)
		c := &collection{id: id, href: r.Href, cal: calendar.Calendar{ID: id, Name: p.DisplayName, Access: "reader"}}
		if c.cal.Name == "" {
			c.cal.Name = id
		}
		if col := p.CalendarColor; len(col) >= 7 && col[0] == '#' {
			c.cal.Color = col[:7] // Apple's #rrggbbaa
		}
		c.cal.Zone = zoneOf(p.CalendarTimezone)
		if p.Privileges != nil {
			for _, pr := range p.Privileges.Privilege {
				if pr.All != nil || pr.Write != nil || pr.Bind != nil {
					c.cal.Access = "owner"
				}
			}
		} else {
			c.cal.Access = "owner"
		}
		if p.Reports != nil {
			for _, rp := range p.Reports.Report {
				c.sync = c.sync || rp.Report.SyncCollection != nil
			}
		}
		out = append(out, c)
	}
	// The first calendar the account may write is its primary one.
	for _, c := range out {
		if c.cal.Access == "owner" {
			c.cal.Primary = true
			break
		}
	}
	m := map[string]*collection{}
	for _, c := range out {
		m[c.id] = c
	}
	x.mu.Lock()
	x.cals = m
	x.mu.Unlock()
	return out, nil
}

// zoneOf takes the TZID of a calendar-timezone's VTIMEZONE, when it names
// an IANA zone; only that one line is looked at.
func zoneOf(vtz string) string {
	for _, line := range strings.Split(vtz, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 5 && strings.EqualFold(line[:5], "TZID:") {
			if z, ok := when.IANA(strings.TrimSpace(line[5:])); ok {
				return z
			}
		}
	}
	return ""
}

// collection finds a calendar by id.
func (x *Session) collection(ctx adapter.Context, id string) (*collection, error) {
	x.mu.Lock()
	c := x.cals[id]
	x.mu.Unlock()
	if c != nil {
		return c, nil
	}
	if _, err := x.calendars(ctx); err != nil {
		return nil, err
	}
	x.mu.Lock()
	c = x.cals[id]
	x.mu.Unlock()
	if c == nil {
		return nil, providers.Errorf(connectors.CodeNotFound, "the account has no calendar %q", id)
	}
	return c, nil
}

// primary is the primary calendar's id, for calls that leave it out.
func (x *Session) primary(ctx adapter.Context) (string, error) {
	cals, err := x.calendars(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range cals {
		if c.cal.Primary {
			return c.id, nil
		}
	}
	if len(cals) > 0 {
		return cals[0].id, nil
	}
	return "", providers.Errorf(connectors.CodeNotFound, "the account has no calendars")
}

// Resource names: our key, or an event id, as a safe file name.
func resourceName(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_', r == '@':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (c *collection) resource(name string) string {
	return strings.TrimSuffix(c.href, "/") + "/" + url.PathEscape(name) + ".ics"
}

// object is one calendar object resource: a one-off, or a series with its
// exceptions.
type object struct {
	href, etag, id string
	events         []calendar.Event
}

// item is a resource's raw answer.
type item struct {
	href, etag, data string
}

// idOf is a resource's event id: its name without .ics.
func idOf(href string) string { return strings.TrimSuffix(lastSegment(href), ".ics") }

// parse reads resources' calendar data in as few child runs as fit:
// their insides joined into one VCALENDAR, events matched back to their
// resource by UID (found by looking at UID lines only); resources sharing
// a UID, or a batch the child refuses, are read one by one.
func (x *Session) parse(ctx adapter.Context, cal string, items []item) ([]object, error) {
	objs := make([]object, len(items))
	uidOf := map[string]int{}
	shared := map[int]bool{}
	for i, it := range items {
		objs[i] = object{href: it.href, etag: it.etag, id: idOf(it.href)}
		for _, u := range uids(it.data) {
			if j, dup := uidOf[u]; dup && j != i {
				shared[i], shared[j] = true, true
			}
			uidOf[u] = i
		}
	}
	place := func(p ical.Parsed, only int) {
		for _, ev := range p.Events {
			i, ok := only, only >= 0
			if !ok {
				i, ok = uidOf[ev.ICalUID]
			}
			if !ok {
				continue
			}
			o := &objs[i]
			suffix := strings.TrimPrefix(ev.ID, ev.ICalUID)
			ev.ID = o.id + suffix
			if ev.RecurrenceID != "" {
				ev.SeriesID = o.id
			}
			ev.Etag, ev.Calendar = o.etag, cal
			o.events = append(o.events, ev)
		}
	}
	var batch []int
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var b strings.Builder
		b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Axiom//CalDAV//EN\r\n")
		for _, i := range batch {
			b.WriteString(inner(items[i].data))
		}
		b.WriteString("END:VCALENDAR\r\n")
		p, err := x.read(ctx, []byte(b.String()))
		if err == nil {
			place(p, -1)
		} else {
			for _, i := range batch {
				p, err := x.read(ctx, []byte(items[i].data))
				if err != nil {
					ctx.Log.Warn(ctx, "a calendar resource could not be read: %v", err)
					continue
				}
				place(p, i)
			}
		}
		batch, size = nil, 0
		return ctx.Err()
	}
	for i, it := range items {
		if shared[i] {
			p, err := x.read(ctx, []byte(it.data))
			if err != nil {
				ctx.Log.Warn(ctx, "a calendar resource could not be read: %v", err)
				continue
			}
			place(p, i)
			continue
		}
		if size+len(it.data) > ical.MaxBytes-4096 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		batch = append(batch, i)
		size += len(it.data)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	for i := range objs {
		sort.SliceStable(objs[i].events, func(a, b int) bool {
			return objs[i].events[a].RecurrenceID == "" && objs[i].events[b].RecurrenceID != ""
		})
	}
	return objs, nil
}

// uids are the UID lines' values of an .ics (unfolded).
func uids(data string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r\n ", ""), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) > 4 && strings.EqualFold(line[:4], "UID:") {
			out = append(out, strings.TrimSpace(line[4:]))
		}
	}
	return out
}

// inner is what lies within an .ics's VCALENDAR, without its own
// top-level properties, so several can be joined into one.
func inner(data string) string {
	var b strings.Builder
	depth := 0
	for _, line := range strings.SplitAfter(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		t := strings.TrimRight(line, "\n")
		up := strings.ToUpper(t)
		switch {
		case strings.HasPrefix(up, "BEGIN:"):
			depth++
			if depth == 1 {
				continue
			}
		case strings.HasPrefix(up, "END:"):
			depth--
			if depth == 0 {
				continue
			}
		}
		if depth <= 1 && !strings.HasPrefix(up, "BEGIN:") && !strings.HasPrefix(up, "END:") {
			continue // VERSION, PRODID, METHOD, CALSCALE and their folds
		}
		if t != "" {
			b.WriteString(t + "\r\n")
		}
	}
	return b.String()
}

// items reads a multistatus's resources with their data.
func items(ms multistatus) []item {
	var out []item
	for _, r := range ms.Responses {
		if !ok(r.Status) {
			continue
		}
		p := r.found()
		if p.CalendarData == "" {
			continue
		}
		out = append(out, item{href: r.Href, etag: p.GetEtag, data: p.CalendarData})
	}
	return out
}

// report sends a REPORT and reads its multistatus.
func (x *Session) report(ctx adapter.Context, what, href, depth, body string) (multistatus, answer, error) {
	a, err := x.c.do(ctx, what, "REPORT", href, map[string]string{"Depth": depth}, body)
	if err != nil {
		return multistatus{}, a, err
	}
	if a.status != http.StatusMultiStatus {
		return multistatus{}, a, a.fail(what)
	}
	ms, err := a.multistatus(what)
	return ms, a, err
}

// fetch gets resources by href with calendar-multiget.
func (x *Session) fetch(ctx adapter.Context, c *collection, hrefs []string) ([]object, error) {
	if len(hrefs) == 0 {
		return nil, nil
	}
	var all []item
	for i := 0; i < len(hrefs); i += 100 {
		ms, _, err := x.report(ctx, "reading changed events", c.href, "1", multiget(hrefs[i:min(i+100, len(hrefs))]))
		if err != nil {
			return nil, err
		}
		all = append(all, items(ms)...)
	}
	return x.parse(ctx, c.id, all)
}

// window lists the objects with an event in the window.
func (x *Session) window(ctx adapter.Context, c *collection, w when.Window) ([]object, error) {
	ms, _, err := x.report(ctx, "listing events", c.href, "1", calendarQuery(stamp(w.From), stamp(w.To), true))
	if err != nil {
		return nil, err
	}
	return x.parse(ctx, c.id, items(ms))
}

func stamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// get reads one object.
func (x *Session) get(ctx adapter.Context, c *collection, name string) (*object, error) {
	href := c.resource(name)
	a, err := x.c.do(ctx, "reading the event", http.MethodGet, href, map[string]string{"Accept": "text/calendar"}, "")
	if err != nil {
		return nil, err
	}
	if a.status != http.StatusOK {
		return nil, a.fail("reading the event")
	}
	objs, err := x.parse(ctx, c.id, []item{{href: href, etag: a.header.Get("ETag"), data: string(a.body)}})
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 || len(objs[0].events) == 0 {
		return nil, providers.Errorf(connectors.CodeUnavailable, "reading the event: the server's resource holds no event that could be read")
	}
	return &objs[0], nil
}

// find reads the object holding an event id and the event's index in it:
// the resource named by the id, else (an exception's id) the one named by
// what precedes its last _.
func (x *Session) find(ctx adapter.Context, c *collection, id string) (*object, int, error) {
	o, err := x.get(ctx, c, id)
	if err == nil {
		for i, ev := range o.events {
			if ev.ID == id {
				return o, i, nil
			}
		}
		return o, 0, nil
	}
	if codeOf(err) != connectors.CodeNotFound {
		return nil, 0, err
	}
	if k := strings.LastIndex(id, "_"); k > 0 {
		if o, err2 := x.get(ctx, c, id[:k]); err2 == nil {
			for i, ev := range o.events {
				if ev.ID == id {
					return o, i, nil
				}
			}
		}
	}
	return nil, 0, err
}

// put writes an object's events, with If-Match (etag) or If-None-Match:
// * (create), and gives its new etag.
func (x *Session) put(ctx adapter.Context, href string, events []calendar.Event, etag string, create bool) (string, int, error) {
	for i := range events {
		events[i].Etag, events[i].Calendar = "", ""
	}
	text, err := ical.Compose("", events, ical.ComposeOptions{Now: x.now(), Resource: true})
	if err != nil {
		return "", 0, providers.Errorf(connectors.CodeInvalid, "%v", err)
	}
	h := map[string]string{"Content-Type": "text/calendar; charset=utf-8"}
	switch {
	case create:
		h["If-None-Match"] = "*"
	case etag != "":
		h["If-Match"] = etag
	}
	a, err := x.c.do(ctx, "saving the event", http.MethodPut, href, h, text)
	if err != nil {
		return "", 0, err
	}
	if a.status != http.StatusCreated && a.status != http.StatusNoContent && a.status != http.StatusOK {
		return "", a.status, a.fail("saving the event")
	}
	return a.header.Get("ETag"), a.status, nil
}

// saved gives an event as the server now holds it.
func (x *Session) saved(ctx adapter.Context, c *collection, name, id string) (json.RawMessage, error) {
	o, err := x.get(ctx, c, name)
	if err != nil {
		return nil, err
	}
	for _, ev := range o.events {
		if ev.ID == id {
			return json.Marshal(ev)
		}
	}
	return json.Marshal(o.events[0])
}

// Call runs one calendar.events operation.
func (x *Session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Calendar  string            `json:"calendar"`
		ID        string            `json:"id"`
		Key       string            `json:"key"`
		Etag      string            `json:"etag"`
		Event     *calendar.Event   `json:"event"`
		Set       json.RawMessage   `json:"set"`
		Answer    string            `json:"answer"`
		Comment   string            `json:"comment"`
		Cursor    string            `json:"cursor"`
		Window    *providers.Window `json:"window"`
		Calendars []string          `json:"calendars"`
		People    []string          `json:"people"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, providers.Errorf(connectors.CodeInvalid, "%v", err)
		}
	}
	switch op {
	case "check":
		if _, err := x.calendars(ctx); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	case "calendar.list":
		cals, err := x.calendars(ctx)
		if err != nil {
			return nil, err
		}
		out := []calendar.Calendar{}
		for _, c := range cals {
			out = append(out, c.cal)
		}
		return json.Marshal(map[string]any{"calendars": out})
	case "freeBusy":
		return x.freeBusy(ctx, a.Calendars, a.People, a.Window)
	}
	if a.Calendar == "" {
		p, err := x.primary(ctx)
		if err != nil {
			return nil, err
		}
		a.Calendar = p
	}
	c, err := x.collection(ctx, a.Calendar)
	if err != nil {
		return nil, err
	}
	switch op {
	case "event.list":
		return x.list(ctx, c, a.Window, a.Cursor)
	case "event.get":
		o, i, err := x.find(ctx, c, a.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(o.events[i])
	case "event.create":
		if a.Event == nil || a.Key == "" {
			return nil, providers.Errorf(connectors.CodeInvalid, "give the key and the event")
		}
		return x.create(ctx, c, a.Key, *a.Event)
	case "event.update":
		return x.update(ctx, c, a.ID, a.Etag, a.Set)
	case "event.delete":
		return x.remove(ctx, c, a.ID, a.Etag)
	case "event.respond":
		o, i, err := x.find(ctx, c, a.ID)
		if err != nil {
			return nil, err
		}
		if err := providers.Answer(&o.events[i], x.self, a.Answer, a.Comment); err != nil {
			return nil, err
		}
		if _, _, err := x.put(ctx, o.href, o.events, o.etag, false); err != nil {
			return nil, err
		}
		return x.saved(ctx, c, o.id, a.ID)
	}
	return nil, providers.Errorf(connectors.CodeNotSupported, "calendar.caldav has no operation %q", op)
}

func (x *Session) create(ctx adapter.Context, c *collection, key string, ev calendar.Event) (json.RawMessage, error) {
	if err := providers.Valid(ev); err != nil {
		return nil, err
	}
	name := resourceName(key)
	ev.ID, ev.Etag = "", ""
	if ev.ICalUID == "" {
		ev.ICalUID = key + "@axiom"
	}
	if ev.SeriesID != "" {
		// An exception joins its series' resource.
		if ev.RecurrenceID == "" {
			return nil, providers.Errorf(connectors.CodeInvalid, "an exception needs its recurrenceId")
		}
		o, _, err := x.find(ctx, c, ev.SeriesID)
		if err != nil {
			return nil, err
		}
		for _, e := range o.events {
			if e.RecurrenceID == ev.RecurrenceID {
				return json.Marshal(e) // made already
			}
		}
		ev.ICalUID = o.events[0].ICalUID
		evs := append(o.events, ev)
		if _, _, err := x.put(ctx, o.href, evs, o.etag, false); err != nil {
			return nil, err
		}
		got, err := x.get(ctx, c, o.id)
		if err != nil {
			return nil, err
		}
		for _, e := range got.events {
			if e.RecurrenceID == ev.RecurrenceID {
				return json.Marshal(e)
			}
		}
		return json.Marshal(got.events[0])
	}
	_, status, err := x.put(ctx, c.resource(name), []calendar.Event{ev}, "", true)
	if err != nil && status != http.StatusPreconditionFailed {
		return nil, err
	}
	// Made now, or by an earlier try of the same key (412): read it back
	// either way, for its id and etag as the server keeps them.
	return x.saved(ctx, c, name, name)
}

func (x *Session) update(ctx adapter.Context, c *collection, id, etag string, set json.RawMessage) (json.RawMessage, error) {
	if len(set) == 0 {
		return nil, providers.Errorf(connectors.CodeInvalid, "give set")
	}
	o, i, err := x.find(ctx, c, id)
	if err != nil {
		return nil, err
	}
	if etag != "" && etag != o.etag {
		return nil, providers.Errorf(connectors.CodeConflict, "the event changed at the service since the version this change was based on")
	}
	before := o.events[i]
	ev, err := providers.Merge(before, set)
	if err != nil {
		return nil, err
	}
	ev.ID, ev.ICalUID, ev.RecurrenceID, ev.SeriesID = before.ID, before.ICalUID, before.RecurrenceID, before.SeriesID
	if err := providers.Valid(ev); err != nil {
		return nil, err
	}
	ev.Sequence = before.Sequence + 1
	o.events[i] = ev
	if etag == "" {
		etag = o.etag
	}
	if _, _, err := x.put(ctx, o.href, o.events, etag, false); err != nil {
		return nil, err
	}
	return x.saved(ctx, c, o.id, id)
}

func (x *Session) remove(ctx adapter.Context, c *collection, id, etag string) (json.RawMessage, error) {
	gone := func() (json.RawMessage, error) { return json.Marshal(map[string]bool{"deleted": false}) }
	o, i, err := x.find(ctx, c, id)
	if codeOf(err) == connectors.CodeNotFound {
		return gone()
	}
	if err != nil {
		return nil, err
	}
	if o.events[i].ID != id {
		return gone()
	}
	if etag != "" && etag != o.etag {
		return nil, providers.Errorf(connectors.CodeConflict, "the event changed at the service since the version this delete was based on")
	}
	if ev := o.events[i]; ev.RecurrenceID != "" {
		// An exception: its series leaves that occurrence out.
		var evs []calendar.Event
		for j, e := range o.events {
			if j == i {
				continue
			}
			if e.RecurrenceID == "" {
				r := when.Recurrence{}
				if e.Recurrence != nil {
					r = *e.Recurrence
				}
				r.ExDate = append(r.ExDate, ev.RecurrenceID)
				e.Recurrence = &r
				e.Sequence++
			}
			evs = append(evs, e)
		}
		if _, _, err := x.put(ctx, o.href, evs, o.etag, false); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"deleted": true})
	}
	h := map[string]string{"If-Match": o.etag}
	if etag != "" {
		h["If-Match"] = etag
	}
	a, err := x.c.do(ctx, "deleting the event", http.MethodDelete, o.href, h, "")
	if err != nil {
		return nil, err
	}
	switch {
	case a.status == http.StatusNotFound:
		return gone()
	case a.status/100 != 2:
		return nil, a.fail("deleting the event")
	}
	return json.Marshal(map[string]bool{"deleted": true})
}

// cursor is event.list's cursor: a sync token, or on a server without
// sync-collection the window and a short hash of each resource's etag.
type cursor struct {
	Token  string            `json:"t,omitempty"`
	From   string            `json:"f,omitempty"`
	To     string            `json:"u,omitempty"`
	Hashes map[string]string `json:"h,omitempty"`
}

func encode(c cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decode(s string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	var c cursor
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	if err != nil || (c.Token == "" && c.From == "") {
		return c, providers.Errorf(connectors.CodeInvalid, "%s: the cursor %q is not one this server gave", calendar.CodeCursorExpired, s)
	}
	return c, nil
}

func short(etag string) string {
	s := sha256.Sum256([]byte(etag))
	return hex.EncodeToString(s[:5])
}

// token reads a calendar's current sync token.
func (x *Session) token(ctx adapter.Context, c *collection) (string, error) {
	a, err := x.c.do(ctx, "reading the sync token", "PROPFIND", c.href, map[string]string{"Depth": "0"}, propfindSyncToken)
	if err != nil {
		return "", err
	}
	if a.status != http.StatusMultiStatus {
		return "", a.fail("reading the sync token")
	}
	ms, err := a.multistatus("reading the sync token")
	if err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		if t := r.found().SyncToken; t != "" {
			return t, nil
		}
	}
	return "", nil
}

// defaultWindow is the window a delta compares on a server without
// sync-collection when the first list named none.
func (x *Session) defaultWindow() when.Window {
	n := x.now().UTC()
	return when.Window{From: n.AddDate(0, -1, 0), To: n.AddDate(1, 0, 0)}
}

// hashes lists a window's resources' etags, without their data.
func (x *Session) hashes(ctx adapter.Context, c *collection, w when.Window) (map[string]string, map[string]string, error) {
	ms, _, err := x.report(ctx, "listing events", c.href, "1", calendarQuery(stamp(w.From), stamp(w.To), false))
	if err != nil {
		return nil, nil, err
	}
	hs, hrefs := map[string]string{}, map[string]string{}
	for _, r := range ms.Responses {
		if !ok(r.Status) {
			continue
		}
		id := idOf(r.Href)
		hs[id] = short(r.found().GetEtag)
		hrefs[id] = r.Href
	}
	return hs, hrefs, nil
}

// newCursor is the cursor for changes after now.
func (x *Session) newCursor(ctx adapter.Context, c *collection, w when.Window, objs []object) (string, error) {
	if c.sync {
		t, err := x.token(ctx, c)
		if err != nil {
			return "", err
		}
		if t != "" {
			return encode(cursor{Token: t}), nil
		}
	}
	hs := map[string]string{}
	if objs == nil {
		h, _, err := x.hashes(ctx, c, w)
		if err != nil {
			return "", err
		}
		hs = h
	} else {
		for _, o := range objs {
			hs[o.id] = short(o.etag)
		}
	}
	return encode(cursor{From: w.From.UTC().Format(time.RFC3339), To: w.To.UTC().Format(time.RFC3339), Hashes: hs}), nil
}

func (x *Session) list(ctx adapter.Context, c *collection, win *providers.Window, cur string) (json.RawMessage, error) {
	if cur != "" {
		evs, removed, next, err := x.delta(ctx, c, cur)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"events": evs, "removed": removed, "cursor": next})
	}
	if win == nil {
		return nil, providers.Errorf(connectors.CodeInvalid, "give a window or a cursor")
	}
	w, err := win.Parse()
	if err != nil {
		return nil, err
	}
	// The token first, so a change made while the window is read comes
	// again in the next delta rather than never.
	var next string
	if c.sync {
		if next, err = x.newCursor(ctx, c, w, nil); err != nil {
			return nil, err
		}
	}
	objs, err := x.window(ctx, c, w)
	if err != nil {
		return nil, err
	}
	if next == "" {
		if next, err = x.newCursor(ctx, c, w, objs); err != nil {
			return nil, err
		}
	}
	events := []calendar.Event{}
	for _, o := range objs {
		keep := false
		for _, ev := range o.events {
			keep = keep || providers.Overlaps(ev, w)
		}
		if keep {
			events = append(events, o.events...)
		}
	}
	return json.Marshal(map[string]any{"events": events, "removed": []string{}, "cursor": next})
}

// delta gives the changes since a cursor.
func (x *Session) delta(ctx adapter.Context, c *collection, cur string) ([]calendar.Event, []string, string, error) {
	old, err := decode(cur)
	if err != nil {
		return nil, nil, "", err
	}
	events, removed := []calendar.Event{}, []string{}
	if old.Token != "" {
		token := old.Token
		var changed []string
		gone := map[string]bool{}
		for round := 0; ; round++ {
			ms, a, err := x.report(ctx, "listing changes", c.href, "1", syncCollection(token))
			if err != nil {
				if ce := connectors.AsError(err); (a.status == http.StatusForbidden || a.status == http.StatusConflict) && strings.Contains(ce.Message, "sync token") {
					return nil, nil, "", providers.Errorf(connectors.CodeInvalid, "%s: the sync token is too old; list the window again", calendar.CodeCursorExpired)
				}
				return nil, nil, "", err
			}
			truncated := false
			for _, r := range ms.Responses {
				st := code(r.Status)
				if strings.TrimSuffix(r.Href, "/") == strings.TrimSuffix(c.href, "/") || strings.HasSuffix(r.Href, "/") {
					truncated = truncated || st == http.StatusInsufficientStorage
					continue
				}
				id := idOf(r.Href)
				if st == http.StatusNotFound {
					gone[id] = true
					continue
				}
				delete(gone, id)
				changed = append(changed, r.Href)
			}
			if ms.SyncToken != "" {
				token = ms.SyncToken
			}
			if !truncated || round >= 50 {
				break
			}
		}
		objs, err := x.fetch(ctx, c, dedupe(changed))
		if err != nil {
			return nil, nil, "", err
		}
		for _, o := range objs {
			events = append(events, o.events...)
		}
		for id := range gone {
			removed = append(removed, id)
		}
		sort.Strings(removed)
		return events, removed, encode(cursor{Token: token}), nil
	}
	from, err1 := time.Parse(time.RFC3339, old.From)
	to, err2 := time.Parse(time.RFC3339, old.To)
	if err1 != nil || err2 != nil {
		return nil, nil, "", providers.Errorf(connectors.CodeInvalid, "%s: the cursor's window is not readable", calendar.CodeCursorExpired)
	}
	w := when.Window{From: from, To: to}
	now, hrefs, err := x.hashes(ctx, c, w)
	if err != nil {
		return nil, nil, "", err
	}
	var changed []string
	for id, h := range now {
		if old.Hashes[id] != h {
			changed = append(changed, hrefs[id])
		}
	}
	// Gone from the window: deleted, or moved out of it (then changed).
	var missing []string
	for id := range old.Hashes {
		if _, ok := now[id]; !ok {
			missing = append(missing, c.resource(id))
		}
	}
	sort.Strings(changed)
	sort.Strings(missing)
	objs, err := x.fetch(ctx, c, append(changed, missing...))
	if err != nil {
		return nil, nil, "", err
	}
	found := map[string]bool{}
	for _, o := range objs {
		found[o.id] = true
		now[o.id] = short(o.etag)
		events = append(events, o.events...)
	}
	for _, href := range missing {
		if id := idOf(href); !found[id] {
			removed = append(removed, id)
		}
	}
	return events, removed, encode(cursor{From: old.From, To: old.To, Hashes: now}), nil
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (x *Session) freeBusy(ctx adapter.Context, cals, people []string, win *providers.Window) (json.RawMessage, error) {
	if win == nil {
		return nil, providers.Errorf(connectors.CodeInvalid, "give a window")
	}
	w, err := win.Parse()
	if err != nil {
		return nil, err
	}
	out := []calendar.FreeBusy{}
	for _, id := range cals {
		c, err := x.collection(ctx, id)
		if err != nil {
			out = append(out, calendar.FreeBusy{Who: id, Busy: []calendar.Busy{}, Error: "the account has no such calendar"})
			continue
		}
		busy, err := x.busyOf(ctx, c, w)
		if err != nil {
			return nil, err
		}
		out = append(out, calendar.FreeBusy{Who: id, Busy: busy})
	}
	for _, p := range people {
		out = append(out, calendar.FreeBusy{Who: p, Busy: []calendar.Busy{}, Error: "CalDAV gives free/busy only for this account's own calendars"})
	}
	return json.Marshal(map[string]any{"freeBusy": out})
}

// busyOf asks the server's free-busy-query, else computes it.
func (x *Session) busyOf(ctx adapter.Context, c *collection, w when.Window) ([]calendar.Busy, error) {
	a, err := x.c.do(ctx, "reading free/busy", "REPORT", c.href, map[string]string{"Depth": "1"}, freeBusyQuery(stamp(w.From), stamp(w.To)))
	if err != nil {
		return nil, err
	}
	if a.status == http.StatusOK && strings.Contains(strings.ToLower(a.header.Get("Content-Type")), "text/calendar") {
		p, err := x.read(ctx, a.body)
		if err == nil {
			out := []calendar.Busy{}
			for _, fb := range p.FreeBusy {
				for _, b := range fb.Busy {
					if b.Type == "" || b.Type == "busy" || b.Type == "unavailable" || b.Type == "tentative" {
						out = append(out, calendar.Busy{Start: b.Start, End: b.End, Type: "busy"})
					}
				}
			}
			return out, nil
		}
		ctx.Log.Warn(ctx, "the server's free/busy could not be read, computing it: %v", err)
	} else if a.status == http.StatusUnauthorized {
		return nil, a.fail("reading free/busy")
	}
	objs, err := x.window(ctx, c, w)
	if err != nil {
		return nil, err
	}
	var evs []calendar.Event
	for _, o := range objs {
		evs = append(evs, o.events...)
	}
	return providers.Busy(evs, x.self, w), nil
}

// Watch polls every calendar's changes every poll interval and raises
// eventChanged and eventRemoved for each; the cursor holds each
// calendar's list cursor.
func (x *Session) Watch(ctx adapter.Context, cur json.RawMessage, emit func(adapter.Event) error) error {
	curs := map[string]string{}
	if len(cur) > 0 {
		_ = json.Unmarshal(cur, &curs)
	}
	for {
		if err := x.poll(ctx, curs, emit); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			ctx.Log.Warn(ctx, "polling the calendars failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(x.every):
		}
	}
}

func (x *Session) poll(ctx adapter.Context, curs map[string]string, emit func(adapter.Event) error) error {
	cals, err := x.calendars(ctx)
	if err != nil {
		return err
	}
	type notice struct{ name, cal, event, etag string }
	var ns []notice
	for _, c := range cals {
		cur, ok := curs[c.id]
		if !ok {
			next, err := x.newCursor(ctx, c, x.defaultWindow(), nil)
			if err != nil {
				return err
			}
			curs[c.id] = next
			continue
		}
		evs, removed, next, err := x.delta(ctx, c, cur)
		if codeOf(err) == connectors.CodeInvalid {
			// An expired token: start again from now; the sync lists its
			// window again on its own expired cursor.
			if next, err = x.newCursor(ctx, c, x.defaultWindow(), nil); err != nil {
				return err
			}
			curs[c.id] = next
			continue
		}
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, ev := range evs {
			if ev.SeriesID != "" || seen[ev.ID] {
				continue
			}
			seen[ev.ID] = true
			ns = append(ns, notice{"eventChanged", c.id, ev.ID, ev.Etag})
		}
		for _, id := range removed {
			ns = append(ns, notice{"eventRemoved", c.id, id, ""})
		}
		curs[c.id] = next
	}
	raw, _ := json.Marshal(curs)
	for i, n := range ns {
		id := fmt.Sprintf("%s-%s-%s-%s-%s", x.conn, n.name, n.cal, n.event, short(n.etag+string(raw)))
		p := map[string]any{"id": id, "calendar": n.cal, "event": n.event}
		if n.etag != "" {
			p["etag"] = n.etag
		}
		payload, _ := json.Marshal(p)
		ev := adapter.Event{Contract: calendar.EventsRef, Name: n.name, ID: id, Payload: payload}
		if i == len(ns)-1 {
			ev.Cursor = raw
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
	return nil
}

// codeOf is err's connector error code, "" for nil.
func codeOf(err error) string {
	if e := connectors.AsError(err); e != nil {
		return e.Code
	}
	return ""
}
