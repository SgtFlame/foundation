// Package ics is the calendar.ics connector type (forge 28l): a subscribed
// iCalendar feed (holidays, a public schedule) read over HTTPS as one
// read-only calendar. The feed is fetched with If-None-Match and
// If-Modified-Since, parsed in ical's child process (a feed is a
// stranger's file), and filtered to the window; every write is
// notSupported. The cursor is the feed's etag and a short hash per event,
// so a delta names what changed and what went.
package ics

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
)

// TypeID is the connector type.
const TypeID = "calendar.ics"

// CalendarID is the feed's one calendar.
const CalendarID = "feed"

// SettingsSchema is calendar.ics's settings.
const SettingsSchema = `{"type":"object","properties":{
  "url":{"type":"string","title":"Feed URL","description":"The feed's address; webcal:// is read as https://"},
  "name":{"type":"string","title":"Name","description":"The calendar's name; the feed's host when empty"},
  "zone":{"type":"string","title":"Time zone","description":"The calendar's zone for all-day and floating times, e.g. Europe/London"},
  "refreshMinutes":{"type":"integer","title":"Refresh every (minutes)","minimum":5,"description":"How often the feed is fetched for changes; 60 when empty"}},
  "required":["url"]}`

// Type is calendar.ics.
func Type() connectors.Type {
	return connectors.Type{
		ID: TypeID, Contracts: []string{calendar.EventsRef}, Source: connectors.SourceBuiltin, Settings: json.RawMessage(SettingsSchema),
		Meta: connectors.Meta{Title: "Subscribed calendar (ICS feed)", Description: "A read-only calendar from an iCalendar feed URL (holidays, a team's public schedule, webcal:// links): implements calendar.events@1 by fetching the feed when it changed, reading it in a limited child process and listing its events in a window; creating, changing and answering are not supported. Changes are found by refetching it every refresh interval.", Tags: []string{"calendar", "ics", "webcal", "feed"}},
	}
}

// Settings are a connection's settings.
type Settings struct {
	URL            string `json:"url"`
	Name           string `json:"name"`
	Zone           string `json:"zone"`
	RefreshMinutes int    `json:"refreshMinutes"`
}

// Options are for tests: HTTP the client, Read the .ics reader (the child
// when nil), Refresh replacing the refresh interval.
type Options struct {
	HTTP    *http.Client
	Read    ical.Reader
	Refresh time.Duration
}

// MaxFeed is the largest feed read (ical.MaxBytes).
const MaxFeed = ical.MaxBytes

// Impl is the adapter implementation.
func Impl(o Options) adapter.Impl {
	return adapter.Impl{Type: TypeID, Open: func(ctx adapter.Context, conn adapter.Connection) (adapter.Session, error) {
		var st Settings
		if len(conn.Settings) > 0 {
			if err := json.Unmarshal(conn.Settings, &st); err != nil {
				return nil, providers.Errorf(connectors.CodeInvalid, "the connection's settings are not valid: %v", err)
			}
		}
		u, err := FeedURL(st.URL)
		if err != nil {
			return nil, err
		}
		hc := o.HTTP
		if hc == nil {
			hc = &http.Client{Timeout: providers.DefaultTimeout}
		}
		every := o.Refresh
		if every == 0 {
			m := st.RefreshMinutes
			if m <= 0 {
				m = 60
			}
			every = time.Duration(max(m, 5)) * time.Minute
		}
		return &session{st: st, url: u, hc: hc, read: providers.Reader(o.Read), every: every, conn: conn.ID}, nil
	}}
}

// FeedURL checks a feed's address, taking webcal:// as https://.
func FeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", providers.Errorf(connectors.CodeInvalid, "the feed URL is required")
	}
	if rest, ok := strings.CutPrefix(raw, "webcal://"); ok {
		raw = "https://" + rest
	} else if rest, ok := strings.CutPrefix(raw, "webcals://"); ok {
		raw = "https://" + rest
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", providers.Errorf(connectors.CodeInvalid, "the feed URL %q is not an http(s) or webcal address", raw)
	}
	return u.String(), nil
}

type session struct {
	st    Settings
	url   string
	hc    *http.Client
	read  ical.Reader
	every time.Duration
	conn  string

	mu   sync.Mutex
	feed *feed // the last fetch, for conditional requests
}

// feed is one fetch's result.
type feed struct {
	etag, modified string
	events         []calendar.Event
}

func (x *session) Close() error { return nil }

// cursor is event.list's cursor: the feed's validators and a short hash
// per event id.
type cursor struct {
	Etag     string            `json:"e,omitempty"`
	Modified string            `json:"m,omitempty"`
	Hashes   map[string]string `json:"h"`
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
	if err != nil || c.Hashes == nil {
		return c, providers.Errorf(connectors.CodeInvalid, "%s: the cursor %q is not one this feed gave", calendar.CodeCursorExpired, s)
	}
	return c, nil
}

func hash(ev calendar.Event) string {
	ev.Etag = ""
	raw, _ := json.Marshal(ev)
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:6])
}

func cursorOf(f *feed) cursor {
	c := cursor{Etag: f.etag, Modified: f.modified, Hashes: map[string]string{}}
	for _, ev := range f.events {
		c.Hashes[ev.ID] = hash(ev)
	}
	return c
}

// fetch gets the feed, conditionally on etag and modified: changed false
// (and the cached feed) when the server says it did not change.
func (x *session) fetch(ctx adapter.Context, etag, modified string) (*feed, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, x.url, nil)
	if err != nil {
		return nil, false, providers.Errorf(connectors.CodeInvalid, "the feed URL: %v", err)
	}
	req.Header.Set("Accept", "text/calendar")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	resp, err := x.hc.Do(req)
	if err != nil {
		return nil, false, providers.Transport("fetching the feed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		x.mu.Lock()
		f := x.feed
		x.mu.Unlock()
		if f != nil && f.etag == etag && f.modified == modified {
			return f, false, nil
		}
		// Unchanged since the cursor, though this session holds no copy
		// (events nil): a delta needs none, and current fetches it whole.
		return &feed{etag: etag, modified: modified}, false, nil
	}
	if err := providers.Status(resp.StatusCode, "fetching the feed", ""); err != nil {
		return nil, false, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeed+1))
	if err != nil {
		return nil, false, providers.Transport("reading the feed", err)
	}
	if len(raw) > MaxFeed {
		return nil, false, providers.Errorf(connectors.CodeInvalid, "the feed is larger than %d KB", MaxFeed>>10)
	}
	p, err := x.read(ctx, raw)
	if err != nil {
		return nil, false, providers.Errorf(connectors.CodeInvalid, "the feed could not be read: %v", err)
	}
	f := &feed{etag: resp.Header.Get("ETag"), modified: resp.Header.Get("Last-Modified"), events: []calendar.Event{}}
	for _, ev := range p.Events {
		ev.Calendar = CalendarID
		ev.Etag = hash(ev)
		f.events = append(f.events, ev)
	}
	sort.SliceStable(f.events, func(i, j int) bool { return f.events[i].ID < f.events[j].ID })
	x.mu.Lock()
	x.feed = f
	x.mu.Unlock()
	return f, true, nil
}

// current is the feed, fetched again only when it changed.
func (x *session) current(ctx adapter.Context) (*feed, error) {
	x.mu.Lock()
	f := x.feed
	x.mu.Unlock()
	etag, mod := "", ""
	if f != nil {
		etag, mod = f.etag, f.modified
	}
	f, _, err := x.fetch(ctx, etag, mod)
	if err == nil && f.events == nil && (etag != "" || mod != "") {
		// A server answering 304 to validators this session never cached.
		f, _, err = x.fetch(ctx, "", "")
	}
	return f, err
}

func (x *session) name() string {
	if x.st.Name != "" {
		return x.st.Name
	}
	if u, err := url.Parse(x.url); err == nil {
		return u.Host
	}
	return "Feed"
}

func (x *session) Call(ctx adapter.Context, op string, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Calendar  string            `json:"calendar"`
		ID        string            `json:"id"`
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
	if a.Calendar != "" && a.Calendar != CalendarID {
		return nil, providers.Errorf(connectors.CodeNotFound, "this feed has one calendar, %q", CalendarID)
	}
	switch op {
	case "check":
		if _, err := x.current(ctx); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]bool{"ok": true})
	case "calendar.list":
		return json.Marshal(map[string]any{"calendars": []calendar.Calendar{{ID: CalendarID, Name: x.name(), Zone: x.st.Zone, Access: "reader", Primary: true}}})
	case "event.list":
		return x.list(ctx, a.Window, a.Cursor)
	case "event.get":
		f, err := x.current(ctx)
		if err != nil {
			return nil, err
		}
		for _, ev := range f.events {
			if ev.ID == a.ID {
				return json.Marshal(ev)
			}
		}
		return nil, providers.Errorf(connectors.CodeNotFound, "the feed has no event %q", a.ID)
	case "freeBusy":
		if a.Window == nil {
			return nil, providers.Errorf(connectors.CodeInvalid, "give a window")
		}
		w, err := a.Window.Parse()
		if err != nil {
			return nil, err
		}
		f, err := x.current(ctx)
		if err != nil {
			return nil, err
		}
		out := []calendar.FreeBusy{}
		for _, c := range a.Calendars {
			if c != CalendarID {
				out = append(out, calendar.FreeBusy{Who: c, Busy: []calendar.Busy{}, Error: "this feed has no such calendar"})
				continue
			}
			out = append(out, calendar.FreeBusy{Who: c, Busy: providers.Busy(f.events, "", w)})
		}
		for _, p := range a.People {
			out = append(out, calendar.FreeBusy{Who: p, Busy: []calendar.Busy{}, Error: "a feed knows nobody's free time"})
		}
		return json.Marshal(map[string]any{"freeBusy": out})
	case "event.create", "event.update", "event.delete", "event.respond":
		return nil, providers.ReadOnly(op)
	}
	return nil, providers.Errorf(connectors.CodeNotSupported, "calendar.ics has no operation %q", op)
}

func (x *session) list(ctx adapter.Context, win *providers.Window, cur string) (json.RawMessage, error) {
	if cur != "" {
		old, err := decode(cur)
		if err != nil {
			return nil, err
		}
		f, changed, err := x.fetch(ctx, old.Etag, old.Modified)
		if err != nil {
			return nil, err
		}
		events, removed := []calendar.Event{}, []string{}
		if !changed {
			return json.Marshal(map[string]any{"events": events, "removed": removed, "cursor": cur})
		}
		now := cursorOf(f)
		for _, ev := range f.events {
			if old.Hashes[ev.ID] != now.Hashes[ev.ID] {
				events = append(events, ev)
			}
		}
		for id := range old.Hashes {
			if _, ok := now.Hashes[id]; !ok {
				removed = append(removed, id)
			}
		}
		sort.Strings(removed)
		return json.Marshal(map[string]any{"events": events, "removed": removed, "cursor": encode(now)})
	}
	if win == nil {
		return nil, providers.Errorf(connectors.CodeInvalid, "give a window or a cursor")
	}
	w, err := win.Parse()
	if err != nil {
		return nil, err
	}
	f, err := x.current(ctx)
	if err != nil {
		return nil, err
	}
	events := []calendar.Event{}
	series := map[string]bool{}
	for _, ev := range f.events {
		if ev.SeriesID == "" && providers.Overlaps(ev, w) {
			events = append(events, ev)
			series[ev.ID] = true
		}
	}
	for _, ev := range f.events {
		if ev.SeriesID != "" && (series[ev.SeriesID] || providers.Overlaps(ev, w)) {
			events = append(events, ev)
		}
	}
	return json.Marshal(map[string]any{"events": events, "removed": []string{}, "cursor": encode(cursorOf(f))})
}

// Watch fetches the feed every refresh interval and raises eventChanged
// and eventRemoved for what changed since the cursor.
func (x *session) Watch(ctx adapter.Context, cur json.RawMessage, emit func(adapter.Event) error) error {
	var c cursor
	if len(cur) > 0 {
		_ = json.Unmarshal(cur, &c)
	}
	first := c.Hashes == nil
	for {
		f, changed, err := x.fetch(ctx, c.Etag, c.Modified)
		switch {
		case err != nil:
			ctx.Log.Warn(ctx, "the feed could not be fetched: %v", err)
		case first:
			c, first = cursorOf(f), false
		case changed:
			now := cursorOf(f)
			type notice struct {
				name, event, etag string
			}
			var ns []notice
			for _, ev := range f.events {
				if c.Hashes[ev.ID] != now.Hashes[ev.ID] {
					ns = append(ns, notice{"eventChanged", ev.ID, now.Hashes[ev.ID]})
				}
			}
			var gone []string
			for id := range c.Hashes {
				if _, ok := now.Hashes[id]; !ok {
					gone = append(gone, id)
				}
			}
			sort.Strings(gone)
			for _, id := range gone {
				ns = append(ns, notice{"eventRemoved", id, ""})
			}
			raw, _ := json.Marshal(now)
			for i, n := range ns {
				id := fmt.Sprintf("%s-%s-%s-%s", x.conn, n.name, n.event, n.etag)
				p := map[string]any{"id": id, "calendar": CalendarID, "event": n.event}
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
			c = now
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(x.every):
		}
	}
}
