package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// Client calls one provider's JSON API: Base is its root URL, Auth sets
// the credentials on each request (never logged, never in an error), and
// HTTP is the client, whose transport tests replace with a cassette.
type Client struct {
	Base string
	HTTP *http.Client
	Auth func(*http.Request)
	// Message reads the provider's error text from an error body; the
	// status text when nil or empty.
	Message func(body []byte) string
}

// DefaultTimeout bounds one request when the client gives none.
const DefaultTimeout = 60 * time.Second

// Status maps an HTTP status to a connector error code; "" for success.
func Status(code int) string {
	switch {
	case code < 400:
		return ""
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return connectors.CodeAuth
	case code == http.StatusNotFound:
		return connectors.CodeNotFound
	case code == http.StatusConflict || code == http.StatusPreconditionFailed:
		return connectors.CodeConflict
	case code == http.StatusTooManyRequests:
		return connectors.CodeRateLimited
	case code >= 500:
		return connectors.CodeUnavailable
	default:
		return connectors.CodeInvalid
	}
}

// Do sends method path?query with body as JSON (nil for none) and decodes
// a JSON answer into out (nil to ignore it). A failure status is a
// *connectors.Error with the provider's message; a failure to reach it is
// unavailable.
func (c Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) (http.Header, error) {
	u := strings.TrimRight(c.Base, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, connectors.Errorf(connectors.CodeInvalid, "the request is not valid: %v", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeInvalid, "the request is not valid: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Auth != nil {
		c.Auth(req)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the provider cannot be reached: %s", redact(err.Error()))
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, connectors.Errorf(connectors.CodeUnavailable, "the provider's answer was cut off")
	}
	if code := Status(res.StatusCode); code != "" {
		msg := ""
		if c.Message != nil {
			msg = c.Message(raw)
		}
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return res.Header, connectors.Errorf(code, "%s %s: %d %s", method, path, res.StatusCode, msg)
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return res.Header, connectors.Errorf(connectors.CodeUnavailable, "the provider's answer to %s %s is not JSON it should be: %v", method, path, err)
		}
	}
	return res.Header, nil
}

// redact keeps a URL's query (which may carry a token) out of an error.
func redact(s string) string {
	if i := strings.Index(s, "?"); i >= 0 {
		if j := strings.IndexAny(s[i:], "\" "); j >= 0 {
			return s[:i] + s[i+j:]
		}
		return s[:i]
	}
	return s
}
