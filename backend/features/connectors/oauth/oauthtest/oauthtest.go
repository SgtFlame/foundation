// Package oauthtest is an in-process OAuth provider for tests (forge
// 26a-1): authorize (as a function standing in for the person's consent),
// a token endpoint with PKCE, refresh with rotation and invalid_grant, a
// userinfo endpoint and revoke.
package oauthtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// ProviderID is the test provider's id.
const ProviderID = "test"

// Client is the registered app the provider knows.
const (
	ClientID     = "test-client"
	ClientSecret = "test-client-secret"
)

type grant struct {
	challenge, redirect, account string
}

// Server is the provider.
type Server struct {
	*httptest.Server
	// Account is who consents.
	Account string
	// ExpiresIn is each access token's life (an hour when zero).
	ExpiresIn time.Duration
	// Refreshes counts refresh grants answered with new tokens.
	Refreshes atomic.Int64
	// Delay holds each token answer, to let refreshes race.
	Delay time.Duration

	mu      sync.Mutex
	n       int
	codes   map[string]grant
	refresh map[string]string // refresh token → account, while valid
	access  map[string]string // access token → account
	Revoked []string
}

// New starts a provider, closed when t ends.
func New(t testing.TB) *Server {
	s := &Server{Account: "ann@example.com", codes: map[string]grant{}, refresh: map[string]string{}, access: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", s.token)
	mux.HandleFunc("/userinfo", s.userinfo)
	mux.HandleFunc("/revoke", s.revoke)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "use Consent", http.StatusTeapot) })
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Provider is its metadata.
func (s *Server) Provider() connectors.OAuthProvider {
	return connectors.OAuthProvider{ID: ProviderID, Title: "Test provider", AuthorizeURL: s.URL + "/authorize", TokenURL: s.URL + "/token",
		RevokeURL: s.URL + "/revoke", UserInfoURL: s.URL + "/userinfo", AccountField: "email", PKCE: true,
		Scopes: map[string]string{"read": "Reads the test service", "write": "Changes the test service"}, Register: "https://example.com/apps"}
}

// Consent is the person approving on the authorize page: it checks the
// request and gives the query the provider sends back to the redirect URL
// ({code, state}).
func (s *Server) Consent(authorize string) (redirect string, query url.Values, err error) {
	u, err := url.Parse(authorize)
	if err != nil {
		return "", nil, err
	}
	q := u.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" {
		return "", nil, fmt.Errorf("a bad authorize request: %s", u.RawQuery)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return "", nil, fmt.Errorf("no PKCE challenge")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	code := fmt.Sprintf("code-%d", s.n)
	s.codes[code] = grant{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), account: s.Account}
	return q.Get("redirect_uri"), url.Values{"code": {code}, "state": {q.Get("state")}}, nil
}

func fail(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if s.Delay > 0 {
		time.Sleep(s.Delay)
	}
	_ = r.ParseForm()
	if r.PostForm.Get("client_id") != ClientID || r.PostForm.Get("client_secret") != ClientSecret {
		fail(w, "invalid_client")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var account string
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		g, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			fail(w, "invalid_grant")
			return
		}
		account = g.account
	case "refresh_token":
		a, ok := s.refresh[r.PostForm.Get("refresh_token")]
		if !ok {
			fail(w, "invalid_grant")
			return
		}
		delete(s.refresh, r.PostForm.Get("refresh_token")) // rotated
		account = a
		s.Refreshes.Add(1)
	default:
		fail(w, "unsupported_grant_type")
		return
	}
	s.n++
	at, rt := fmt.Sprintf("at-%d", s.n), fmt.Sprintf("rt-%d", s.n)
	s.access[at], s.refresh[rt] = account, account
	life := s.ExpiresIn
	if life == 0 {
		life = time.Hour
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": at, "refresh_token": rt, "token_type": "Bearer", "expires_in": int(life.Seconds()), "scope": "read"})
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	account, ok := s.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"email": account})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := r.PostForm.Get("token")
	delete(s.refresh, tok)
	delete(s.access, tok)
	s.Revoked = append(s.Revoked, tok)
}

// Invalidate forgets every refresh token, as a provider does when access is
// withdrawn.
func (s *Server) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh = map[string]string{}
}

// Type is a connector type that signs in with the test provider.
func Type(contract string) connectors.Type {
	return connectors.Type{ID: "test.oauth", Contracts: []string{contract}, Source: connectors.SourceBuiltin,
		Settings: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string","title":"Label"}}}`),
		OAuth:    &connectors.OAuth{Provider: ProviderID, Scopes: []string{"read"}, Extra: map[string]string{"access_type": "offline"}},
		Meta:     connectors.Meta{Title: "Test OAuth", Description: "Signs in with the test OAuth provider. For tests only.", Tags: []string{"test"}}}
}

// ChoosingType is a type whose connections choose in their signIn setting
// whether to sign in with the test provider (forge 26a-2): empty signs in
// with a password.
func ChoosingType(contract string) connectors.Type {
	return connectors.Type{ID: "test.oauthChoice", Contracts: []string{contract}, Source: connectors.SourceBuiltin,
		Settings: json.RawMessage(`{"type":"object","properties":{"signIn":{"type":"string","title":"Sign in with","enum":["","test"]},"password":{"type":"secret","title":"Password"}}}`),
		OAuth:    &connectors.OAuth{Field: "signIn", Choices: []connectors.OAuthChoice{{Provider: ProviderID, Scopes: []string{"write"}}}},
		Meta:     connectors.Meta{Title: "Test OAuth choice", Description: "Signs in with a password or the test OAuth provider, as each connection chooses. For tests only.", Tags: []string{"test"}}}
}
