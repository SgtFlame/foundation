package caldav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/calendar/providers"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// The WebDAV side of the type: requests with Basic authentication, and the
// multistatus answers of RFC 4918, 4791 and 6578 read with encoding/xml
// (which never fetches entities). A multistatus is the server's protocol
// answer, capped at maxBody; the calendar data inside it is a stranger's
// .ics and goes to the child.

// Namespaces.
const (
	nsDAV    = "DAV:"
	nsCalDAV = "urn:ietf:params:xml:ns:caldav"
	nsApple  = "http://apple.com/ns/ical/"
)

// maxBody caps one answer read.
const maxBody = 16 << 20

type multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []response `xml:"DAV: response"`
	SyncToken string     `xml:"DAV: sync-token"`
}

type response struct {
	Href      string     `xml:"DAV: href"`
	Status    string     `xml:"DAV: status"`
	Propstats []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string `xml:"DAV: status"`
	Prop   prop   `xml:"DAV: prop"`
}

type hrefProp struct {
	Href string `xml:"DAV: href"`
}

type prop struct {
	ResourceType *struct {
		Collection *struct{} `xml:"DAV: collection"`
		Calendar   *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	} `xml:"DAV: resourcetype"`
	DisplayName      string    `xml:"DAV: displayname"`
	GetEtag          string    `xml:"DAV: getetag"`
	SyncToken        string    `xml:"DAV: sync-token"`
	CurrentPrincipal *hrefProp `xml:"DAV: current-user-principal"`
	CalendarHome     *hrefProp `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
	CalendarData     string    `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	CalendarColor    string    `xml:"http://apple.com/ns/ical/ calendar-color"`
	CalendarTimezone string    `xml:"urn:ietf:params:xml:ns:caldav calendar-timezone"`
	Privileges       *struct {
		Privilege []struct {
			All   *struct{} `xml:"DAV: all"`
			Write *struct{} `xml:"DAV: write"`
			Bind  *struct{} `xml:"DAV: bind"`
			Read  *struct{} `xml:"DAV: read"`
		} `xml:"DAV: privilege"`
	} `xml:"DAV: current-user-privilege-set"`
	Components *struct {
		Comp []struct {
			Name string `xml:"name,attr"`
		} `xml:"urn:ietf:params:xml:ns:caldav comp"`
	} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
	Reports *struct {
		Report []struct {
			Report struct {
				SyncCollection *struct{} `xml:"DAV: sync-collection"`
			} `xml:"DAV: report"`
		} `xml:"DAV: supported-report"`
	} `xml:"DAV: supported-report-set"`
}

// ok tells whether a status line (HTTP/1.1 200 OK) is 2xx; an empty one
// is taken as OK.
func ok(status string) bool { return status == "" || code(status)/100 == 2 }

// code reads a status line's code.
func code(status string) int {
	f := strings.Fields(status)
	if len(f) < 2 {
		return 0
	}
	n := 0
	_, _ = fmt.Sscanf(f[1], "%d", &n)
	return n
}

// found is a response's properties that came back 2xx.
func (r response) found() prop {
	var out prop
	for _, ps := range r.Propstats {
		if !ok(ps.Status) {
			continue
		}
		p := ps.Prop
		if p.ResourceType != nil {
			out.ResourceType = p.ResourceType
		}
		if p.DisplayName != "" {
			out.DisplayName = p.DisplayName
		}
		if p.GetEtag != "" {
			out.GetEtag = p.GetEtag
		}
		if p.SyncToken != "" {
			out.SyncToken = p.SyncToken
		}
		if p.CurrentPrincipal != nil {
			out.CurrentPrincipal = p.CurrentPrincipal
		}
		if p.CalendarHome != nil {
			out.CalendarHome = p.CalendarHome
		}
		if p.CalendarData != "" {
			out.CalendarData = p.CalendarData
		}
		if p.CalendarColor != "" {
			out.CalendarColor = p.CalendarColor
		}
		if p.CalendarTimezone != "" {
			out.CalendarTimezone = p.CalendarTimezone
		}
		if p.Privileges != nil {
			out.Privileges = p.Privileges
		}
		if p.Components != nil {
			out.Components = p.Components
		}
		if p.Reports != nil {
			out.Reports = p.Reports
		}
	}
	return out
}

// client speaks WebDAV to one server.
type client struct {
	mu         sync.Mutex
	base       *url.URL
	user, pass string
	hc         *http.Client
}

// answer is a request's answer, its body read.
type answer struct {
	status int
	header http.Header
	body   []byte
}

func (a answer) multistatus(what string) (multistatus, error) {
	var ms multistatus
	if err := xml.NewDecoder(bytes.NewReader(a.body)).Decode(&ms); err != nil {
		return ms, providers.Errorf(connectors.CodeUnavailable, "%s: the calendar service's answer is not a WebDAV multistatus: %v", what, err)
	}
	return ms, nil
}

// resolve makes href (absolute, or a path) absolute against the base.
func (c *client) resolve(href string) (*url.URL, error) {
	u, err := url.Parse(href)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.base.ResolveReference(u), nil
}

// do sends a request, following up to five redirects with its method and
// body (a server's /.well-known/caldav redirects): credentials go only to
// the same host or over https.
func (c *client) do(ctx context.Context, what, method, href string, h map[string]string, body string) (answer, error) {
	u, err := c.resolve(href)
	if err != nil {
		return answer{}, providers.Errorf(connectors.CodeInvalid, "%s: %q is not a URL", what, href)
	}
	origin := u.Host
	for hop := 0; ; hop++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
		if err != nil {
			return answer{}, providers.Errorf(connectors.CodeInvalid, "%s: %v", what, err)
		}
		for k, v := range h {
			req.Header.Set(k, v)
		}
		if body != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
		}
		if c.user != "" && (u.Host == origin || u.Scheme == "https") {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass)))
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) && ctx.Err() != nil {
				return answer{}, ctx.Err()
			}
			return answer{}, providers.Transport(what, err)
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		_ = resp.Body.Close()
		if rerr != nil {
			return answer{}, providers.Transport(what, rerr)
		}
		if len(raw) > maxBody {
			return answer{}, providers.Errorf(connectors.CodeUnavailable, "%s: the calendar service's answer is larger than %d MB", what, maxBody>>20)
		}
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			loc := resp.Header.Get("Location")
			if loc == "" || hop >= 5 {
				return answer{}, providers.Errorf(connectors.CodeUnavailable, "%s: the calendar service redirects without end", what)
			}
			next, err := u.Parse(loc)
			if err != nil {
				return answer{}, providers.Errorf(connectors.CodeUnavailable, "%s: a redirect to %q", what, loc)
			}
			u = next
			if hop == 0 {
				c.mu.Lock()
				c.base = &url.URL{Scheme: u.Scheme, Host: u.Host}
				c.mu.Unlock()
			}
			continue
		}
		return answer{status: resp.StatusCode, header: resp.Header, body: raw}, nil
	}
}

// fail is a non-2xx answer as a connector error.
func (a answer) fail(what string) error {
	detail := ""
	if bytes.Contains(a.body, []byte("valid-sync-token")) {
		detail = "the sync token is no longer valid"
	}
	return providers.Status(a.status, what, detail)
}

const (
	propfindPrincipal = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:current-user-principal/><C:calendar-home-set/><D:resourcetype/></D:prop></D:propfind>`
	propfindHome = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><C:calendar-home-set/></D:prop></D:propfind>`
	propfindCalendars = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="http://apple.com/ns/ical/"><D:prop><D:resourcetype/><D:displayname/><A:calendar-color/><C:calendar-timezone/><D:current-user-privilege-set/><C:supported-calendar-component-set/><D:supported-report-set/></D:prop></D:propfind>`
	propfindSyncToken = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:"><D:prop><D:sync-token/></D:prop></D:propfind>`
)

// calendarQuery asks for a window's events, with their data or only
// their etags.
func calendarQuery(from, to string, data bool) string {
	d := ""
	if data {
		d = "<C:calendar-data/>"
	}
	return `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/>` + d + `</D:prop><C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:time-range start="` + from + `" end="` + to + `"/></C:comp-filter></C:comp-filter></C:filter></C:calendar-query>`
}

// multiget asks for resources' data by href.
func multiget(hrefs []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/><C:calendar-data/></D:prop>`)
	for _, h := range hrefs {
		b.WriteString("<D:href>")
		_ = xml.EscapeText(&b, []byte(h))
		b.WriteString("</D:href>")
	}
	b.WriteString(`</C:calendar-multiget>`)
	return b.String()
}

// syncCollection asks for the changes since a sync token (RFC 6578).
func syncCollection(token string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<D:sync-collection xmlns:D="DAV:"><D:sync-token>`)
	_ = xml.EscapeText(&b, []byte(token))
	b.WriteString(`</D:sync-token><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`)
	return b.String()
}

func freeBusyQuery(from, to string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<C:free-busy-query xmlns:C="urn:ietf:params:xml:ns:caldav"><C:time-range start="` + from + `" end="` + to + `"/></C:free-busy-query>`
}
