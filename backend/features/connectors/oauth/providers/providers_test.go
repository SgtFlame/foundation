package providers_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/oauth"
	"github.com/SgtFlame/foundation/backend/features/connectors/oauth/providers"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestProvidersPassTheChecks(t *testing.T) {
	for _, p := range providers.All() {
		if ps := connectors.CheckOAuthProvider(p); len(ps) > 0 {
			t.Errorf("%s: %v", p.ID, ps)
		}
	}
}

// The choices Mail, Calendar and chat types make, checked against the
// providers as a type's sign-in is.
func TestChoicesPassTheChecks(t *testing.T) {
	settings := json.RawMessage(`{"type":"object","properties":{"oauthProvider":{"type":"string"}}}`)
	ty := connectors.Type{Settings: settings, OAuth: &connectors.OAuth{Field: "oauthProvider", Choices: []connectors.OAuthChoice{
		{Provider: providers.Google, Scopes: []string{providers.GoogleMail}},
		{Provider: providers.Microsoft, Scopes: []string{providers.MicrosoftIMAP, providers.MicrosoftSMTP}},
	}}}
	find := func(id string) (connectors.OAuthProvider, bool) {
		for _, p := range providers.All() {
			if p.ID == id {
				return p, true
			}
		}
		return connectors.OAuthProvider{}, false
	}
	if ps := connectors.CheckOAuth(ty, find); len(ps) > 0 {
		t.Fatal(ps)
	}
	bad := ty
	bad.OAuth = &connectors.OAuth{Field: "missing", Choices: []connectors.OAuthChoice{{Provider: providers.Slack, Scopes: []string{"nope"}}}}
	if ps := connectors.CheckOAuth(bad, find); len(ps) != 2 {
		t.Fatalf("want the missing field and the unknown scope: %v", ps)
	}
	c, ok := ty.OAuth.For(json.RawMessage(`{"oauthProvider":"microsoft"}`))
	if !ok || c.Provider != providers.Microsoft {
		t.Fatalf("%+v %v", c, ok)
	}
	if _, ok := ty.OAuth.For(json.RawMessage(`{"oauthProvider":""}`)); ok {
		t.Fatal("a connection that chose no provider signs in with a password")
	}
}

type sample struct {
	choice connectors.OAuthChoice
	reg    oauth.Registration
}

var samples = map[string]sample{
	providers.Google:    {connectors.OAuthChoice{Provider: providers.Google, Scopes: []string{providers.GoogleMail}}, oauth.Registration{ClientID: "g-client", ClientSecret: "g-secret"}},
	providers.Microsoft: {connectors.OAuthChoice{Provider: providers.Microsoft, Scopes: []string{providers.MicrosoftIMAP, providers.MicrosoftSMTP}}, oauth.Registration{ClientID: "m-client", ClientSecret: "m-secret", Tenant: "organizations"}},
	providers.Slack:     {connectors.OAuthChoice{Provider: providers.Slack, Scopes: []string{"channels:read", "channels:history", "chat:write"}}, oauth.Registration{ClientID: "s-client", ClientSecret: "s-secret"}},
}

// Each provider's authorize URL and token request match their golden
// files (testdata/golden, -update to accept a deliberate change).
func TestRequestShapes(t *testing.T) {
	for _, p := range providers.All() {
		s := samples[p.ID]
		u := oauth.AuthorizeURL(p, s.choice, s.reg, "https://axiom.example.com"+oauth.CallbackPath, "state-1", "verifier-1")
		golden(t, p.ID+".authorize.json", describeURL(t, u))
		form := url.Values{"grant_type": {"authorization_code"}, "code": {"code-1"}, "redirect_uri": {"https://axiom.example.com" + oauth.CallbackPath}}
		if p.PKCE {
			form.Set("code_verifier", "verifier-1")
		}
		req := oauth.TokenRequest(context.Background(), p, s.reg, form)
		body, _ := io.ReadAll(req.Body)
		parsed, _ := url.ParseQuery(string(body))
		user, pass, basic := req.BasicAuth()
		out := map[string]any{"method": req.Method, "url": req.URL.String(), "contentType": req.Header.Get("Content-Type"), "form": flat(parsed)}
		if basic {
			out["basic"] = user + ":" + pass
		}
		raw, _ := json.Marshal(out)
		golden(t, p.ID+".token.json", raw)
	}
}

func describeURL(t *testing.T, u string) json.RawMessage {
	t.Helper()
	pu, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := pu.Query()
	pu.RawQuery = ""
	raw, _ := json.Marshal(map[string]any{"endpoint": pu.String(), "query": flat(q)})
	return raw
}

func flat(v url.Values) map[string]string {
	out := map[string]string{}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = strings.Join(v[k], "|")
	}
	return out
}

func idToken(claims map[string]any) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	b, _ := json.Marshal(claims)
	return h + "." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

// Each provider's account is read from a token response shaped like the
// provider's own (the responses in testdata/cassettes).
func TestAccounts(t *testing.T) {
	want := map[string]string{providers.Google: "ann@example.com", providers.Microsoft: "ann@contoso.com", providers.Slack: "T0123/U0456"}
	for _, p := range providers.All() {
		raw, err := os.ReadFile(filepath.Join("testdata", "cassettes", p.ID+".token.json"))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if c, ok := body["id_token_claims"].(map[string]any); ok { // the cassette keeps claims readable
			body["id_token"] = idToken(c)
		}
		got, err := oauth.ReadAccount(p.AccountField, body)
		if err != nil || got != want[p.ID] {
			t.Errorf("%s: %q %v, want %q", p.ID, got, err, want[p.ID])
		}
	}
	if _, err := oauth.ReadAccount("id_token.email", map[string]any{"id_token": "not-a-jwt"}); err == nil {
		t.Error("a malformed id_token gave an account")
	}
	if _, err := oauth.ReadAccount("{team.id}/{missing}", map[string]any{"team": map[string]any{"id": "T1"}}); err == nil {
		t.Error("a template with a missing field gave an account")
	}
}

func golden(t *testing.T, name string, got json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, got, "", "  "); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	buf.WriteByte('\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", name, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("%s differs from its golden file (run with -update to accept):\n%s", name, buf.String())
	}
}

// Each provider's token, refresh and error answers (testdata/cassettes,
// written from the providers' documentation) are read as the sign-in
// reads them: the tokens with their expiry, a refresh keeping no refresh
// token when none comes, and the error code (Slack's comes with 200).
func TestTokenAnswers(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	for _, p := range providers.All() {
		for _, kind := range []string{"token", "refresh", "error"} {
			raw, err := os.ReadFile(filepath.Join("testdata", "cassettes", p.ID+"."+kind+".json"))
			if err != nil {
				t.Fatal(err)
			}
			status, body := http.StatusOK, raw
			if kind == "error" {
				var e struct {
					Status int             `json:"status"`
					Body   json.RawMessage `json:"body"`
				}
				_ = json.Unmarshal(raw, &e)
				status, body = e.Status, e.Body
			}
			var path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write(body)
			}))
			pt := p
			u, _ := url.Parse(p.TokenURL)
			pt.TokenURL = srv.URL + u.Path
			s := samples[p.ID]
			d := oauth.Deps{HTTP: srv.Client(), Now: func() time.Time { return now }}
			tok, code := d.Exchange(context.Background(), pt, s.reg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rt"}})
			srv.Close()
			if want := p.Endpoint(u.Path, s.reg.Tenant); path != want {
				t.Errorf("%s %s: posted to %s, want %s", p.ID, kind, path, want)
			}
			switch kind {
			case "error":
				if code == "" {
					t.Errorf("%s: an error answer gave tokens %+v", p.ID, tok)
				}
			default:
				if code != "" || tok.AccessToken == "" || tok.Provider != p.ID || tok.ExpiresAt.IsZero() && p.ID != providers.Slack {
					t.Errorf("%s %s: %+v %q", p.ID, kind, tok, code)
				}
				if kind == "refresh" && p.ID == providers.Google && tok.RefreshToken != "" {
					t.Errorf("google's refresh gives no new refresh token: %+v", tok)
				}
			}
		}
	}
}
