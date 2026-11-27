// Package cassette replays and records a provider's HTTP exchanges for
// the infra.compute@1 contract suite (forge 22g: "recorded responses").
// A cassette is a JSON file of request and response pairs, replayed in
// order: each request the type sends must be the next one recorded (its
// method, URL and query, and the recorded body's fields), and gets the
// recorded answer. Recording (INFRA_RECORD=1, with a provider's
// credentials in the environment) writes what a real API answered, with
// no request headers and with secret fields redacted.
package cassette

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Cassette is the file's content.
type Cassette struct {
	Provider string `json:"provider"`
	// Source says where the answers come from: "recorded <date>" from a
	// real account, or the API reference they were written from.
	Source       string        `json:"source"`
	Interactions []Interaction `json:"interactions"`
}

// Interaction is one exchange.
type Interaction struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Request is what was sent: a body's fields given here must be in the
// request's body with these values (other fields may be there too).
type Request struct {
	Method string          `json:"method"`
	URL    string          `json:"url"` // scheme, host and path
	Query  string          `json:"query,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Response is what came back.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// KeptHeaders are the response headers a recording keeps.
var KeptHeaders = []string{"Location", "Azure-AsyncOperation", "Retry-After", "Content-Type"}

// SecretFields are JSON fields whose values a recording replaces.
var SecretFields = []string{"access_token", "refresh_token", "password", "adminPassword", "private_key", "client_secret", "token"}

// Recording tells whether this run records (INFRA_RECORD=1).
func Recording() bool { return os.Getenv("INFRA_RECORD") == "1" }

// Load reads a cassette.
func Load(path string) (Cassette, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Cassette{}, err
	}
	var c Cassette
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cassette{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Player replays a cassette.
type Player struct {
	t    testing.TB
	path string
	mu   sync.Mutex
	c    Cassette
	next int
}

// Replay is a transport replaying the cassette at path; the test fails
// on a request it does not expect, and at its end if any recorded one
// was not sent.
func Replay(t testing.TB, path string) *Player {
	t.Helper()
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &Player{t: t, path: path, c: c}
	t.Cleanup(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.next < len(p.c.Interactions) {
			r := p.c.Interactions[p.next].Request
			t.Errorf("%s: %d of %d exchanges were not sent; the next is %s %s", path, len(p.c.Interactions)-p.next, len(p.c.Interactions), r.Method, r.URL)
		}
	})
	return p
}

// Client is an HTTP client over the player.
func (p *Player) Client() *http.Client { return &http.Client{Transport: p} }

// RoundTrip answers the next recorded exchange.
func (p *Player) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= len(p.c.Interactions) {
		p.t.Errorf("%s: unexpected %s %s after the last exchange", p.path, req.Method, urlOf(req))
		return nil, fmt.Errorf("cassette %s has no more exchanges", p.path)
	}
	in := p.c.Interactions[p.next]
	if why := in.Request.mismatch(req, body); why != "" {
		p.t.Errorf("%s: exchange %d: %s", p.path, p.next+1, why)
		return nil, fmt.Errorf("cassette %s: %s", p.path, why)
	}
	p.next++
	return in.Response.http(req), nil
}

func urlOf(req *http.Request) string {
	return req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
}

func (r Request) mismatch(req *http.Request, body []byte) string {
	if r.Method != req.Method || r.URL != urlOf(req) || r.Query != req.URL.RawQuery {
		q := ""
		if req.URL.RawQuery != "" {
			q = "?" + req.URL.RawQuery
		}
		rq := ""
		if r.Query != "" {
			rq = "?" + r.Query
		}
		return fmt.Sprintf("sent %s %s%s, recorded %s %s%s", req.Method, urlOf(req), q, r.Method, r.URL, rq)
	}
	if len(r.Body) == 0 {
		return ""
	}
	var want, got any
	if err := json.Unmarshal(r.Body, &want); err != nil {
		return "the recorded body is not JSON"
	}
	if form, ok := want.(string); ok { // a form body: its pairs but secrets
		sent := map[string]string{}
		for _, p := range strings.Split(string(body), "&") {
			k, v, _ := strings.Cut(p, "=")
			sent[k] = v
		}
		for _, p := range strings.Split(form, "&") {
			k, v, _ := strings.Cut(p, "=")
			if v == "REDACTED" {
				if _, ok := sent[k]; !ok {
					return fmt.Sprintf("%s %s: the form has no %s", req.Method, r.URL, k)
				}
				continue
			}
			if sent[k] != v {
				return fmt.Sprintf("%s %s: the form's %s differs from the recording", req.Method, r.URL, k)
			}
		}
		return ""
	}
	if err := json.Unmarshal(body, &got); err != nil {
		return fmt.Sprintf("%s %s sent a body that is not JSON: %q", req.Method, r.URL, body)
	}
	if path := subset(want, got, "body"); path != "" {
		return fmt.Sprintf("%s %s: %s differs from the recording (sent %s)", req.Method, r.URL, path, body)
	}
	return ""
}

// subset gives the path where want is not within got, "" when it is.
func subset(want, got any, path string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path
		}
		for k, v := range w {
			gv, ok := g[k]
			if !ok {
				return path + "." + k
			}
			if p := subset(v, gv, path+"."+k); p != "" {
				return p
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(want, got) {
			return path
		}
		return ""
	}
}

func (r Response) http(req *http.Request) *http.Response {
	h := http.Header{}
	for k, v := range r.Headers {
		h.Set(k, v)
	}
	if h.Get("Content-Type") == "" && len(r.Body) > 0 {
		h.Set("Content-Type", "application/json")
	}
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: h,
		Body: io.NopCloser(bytes.NewReader(r.Body)), ContentLength: int64(len(r.Body)), Request: req, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
}

// Recorder records what a real transport answers.
type Recorder struct {
	Next http.RoundTripper
	// From and To rebase recorded URLs: a URL starting with From (the real
	// account's address) is written starting with To (the cassette's).
	From, To string
	mu       sync.Mutex
	c        Cassette
}

// Record records through next (http.DefaultTransport when nil) and saves
// the cassette at path when the test ends.
func Record(t testing.TB, path, provider, source string, next http.RoundTripper) *Recorder {
	if next == nil {
		next = http.DefaultTransport
	}
	r := &Recorder{Next: next, c: Cassette{Provider: provider, Source: source}}
	t.Cleanup(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		raw, _ := json.MarshalIndent(r.c, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Error(err)
		}
	})
	return r
}

// Client is an HTTP client over the recorder.
func (r *Recorder) Client() *http.Client { return &http.Client{Transport: r} }

// RoundTrip sends the request and records it.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	res, err := r.Next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(raw))
	u := urlOf(req)
	if r.From != "" && strings.HasPrefix(u, r.From) {
		u = r.To + strings.TrimPrefix(u, r.From)
	}
	in := Interaction{Request: Request{Method: req.Method, URL: u, Query: req.URL.RawQuery, Body: Redact(body)},
		Response: Response{Status: res.StatusCode, Body: Redact(raw)}}
	for _, k := range KeptHeaders {
		if v := res.Header.Get(k); v != "" {
			if in.Response.Headers == nil {
				in.Response.Headers = map[string]string{}
			}
			in.Response.Headers[k] = v
		}
	}
	r.mu.Lock()
	r.c.Interactions = append(r.c.Interactions, in)
	r.mu.Unlock()
	return res, nil
}

// Redact gives a JSON body with SecretFields' values replaced (a form
// body or other text is kept only as a JSON string with its secret pairs
// replaced); nil for an empty body.
func Redact(body []byte) json.RawMessage {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		parts := strings.Split(string(body), "&")
		for i, p := range parts {
			k, _, _ := strings.Cut(p, "=")
			for _, f := range SecretFields {
				if k == f {
					parts[i] = k + "=REDACTED"
				}
			}
		}
		raw, _ := json.Marshal(strings.Join(parts, "&"))
		return raw
	}
	raw, _ := json.Marshal(redact(v))
	return raw
}

func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			secret := false
			for _, f := range SecretFields {
				secret = secret || k == f
			}
			if secret {
				x[k] = "REDACTED"
			} else {
				x[k] = redact(vv)
			}
		}
	case []any:
		for i := range x {
			x[i] = redact(x[i])
		}
	}
	return v
}
