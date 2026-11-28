// Package oauth is OAuth sign-in for connections (forge 26a-1,
// documentation/Connectors.md › Built in forge 26a-1): the
// authorization-code flow with PKCE for any connector type that names a
// provider (connectors.Type.OAuth), the tenant's app registrations in the
// connectors.oauth section, single-use states in KV cx_oauth_<tenant>,
// tokens kept as the connection's oauth secret by connection.setTokens,
// and refresh before they expire under a lock every process shares.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("connectors").Tags("oauth")

// CallbackPath is where providers send the person back: under /api so the
// session cookie comes with it.
const CallbackPath = "/api/v1/connectors/oauth/callback"

// StateTTL is how long a sign-in may take.
const StateTTL = 10 * time.Minute

// RefreshBefore is how long before expiry a token is refreshed.
const RefreshBefore = 5 * time.Minute

// Deps are what sign-in needs.
type Deps struct {
	// Connections are the connections' deps (projections, bus, secrets,
	// registry, configuration and the extensions with the providers).
	Connections func() connections.Deps
	// PublicBase is the server's public URL (https://axiom.example.com),
	// for the redirect URL when a registration names none.
	PublicBase string
	// HTTP calls the providers; http.DefaultClient when nil.
	HTTP *http.Client
	// Now is the clock, for tests.
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return http.DefaultClient
}

// state is a sign-in on its way, by its id.
type state struct {
	Connection string    `json:"connection"`
	Tenant     uint64    `json:"tenant,string"`
	User       uint64    `json:"user,string"`
	Provider   string    `json:"provider"`
	Verifier   string    `json:"verifier"`
	Redirect   string    `json:"redirect"`
	Return     string    `json:"return,omitempty"`
	At         time.Time `json:"at"`
}

func statesKV(ctx context.Context, js jetstream.JetStream, tenant uint64) (jetstream.KeyValue, error) {
	return connectors.KV(ctx, js, natsd.KV(jetstream.KeyValueConfig{Bucket: fmt.Sprintf("cx_oauth_%d", tenant), History: 1, TTL: StateTTL,
		Description: "OAuth sign-ins on their way: <state> → the connection, the person and the PKCE verifier, single use (forge 26a-1)"}))
}

// stateKey is a state's id: 192 random bits, as hex (a KV key).
func stateKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// challenge is PKCE's S256 code challenge.
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// signInOf is a connection with its type's sign-in and provider.
type signInOf struct {
	c        connections.Connection
	choice   connectors.OAuthChoice
	provider connectors.OAuthProvider
}

func lookup(args features.ServiceArgs, cd connections.Deps, id string) (signInOf, error) {
	ctx, tenant := args.Context(), args.Principal.TenantID
	c, ok, err := connections.Get(ctx, cd.Projections, tenant, id)
	if err != nil {
		return signInOf{}, log.Wrap(err)
	}
	if !ok {
		return signInOf{}, log.Code("connection_not_found").Errorf("there is no connection %s", id)
	}
	if err := connections.CheckAccess(args, c, "manage"); err != nil {
		return signInOf{}, err
	}
	t, ok, err := cd.Registry.Type(ctx, tenant, c.Type)
	if err != nil {
		return signInOf{}, log.Wrap(err)
	}
	if !ok || t.Type.OAuth == nil {
		return signInOf{}, log.Code("oauth_not_supported").Errorf("connection %s's type signs in without OAuth", c.Name)
	}
	settings, _ := json.Marshal(c.Settings)
	choice, ok := t.Type.OAuth.For(settings)
	if !ok {
		return signInOf{}, log.Code("oauth_not_chosen").Errorf("connection %s signs in without OAuth: choose a provider in its %s setting", c.Name, t.Type.OAuth.Field)
	}
	p, ok := connectors.OAuthProviderOf(cd.Extensions, choice.Provider)
	if !ok {
		return signInOf{}, log.Code("oauth_provider_unknown").Errorf("there is no OAuth provider %s", choice.Provider)
	}
	return signInOf{c: c, choice: choice, provider: p}, nil
}

// registration is the provider's app registration the tenant uses (its
// own, else the app scope's default): personal connections use it too.
func registration(args features.ServiceArgs, cd connections.Deps, p connectors.OAuthProvider) (Registration, error) {
	if cd.Config == nil {
		return Registration{}, log.Code("oauth_not_registered").Errorf("no app is registered with %s", p.Title)
	}
	regs, err := config.Read[map[string]Registration](cd.Config, args, SectionID)
	if err != nil {
		return Registration{}, log.Wrap(err)
	}
	r := regs[p.ID]
	if r.ClientID == "" {
		msg := fmt.Sprintf("no app is registered with %s: an administrator sets its client id and secret in Settings › System › Connectors › OAuth apps", p.Title)
		if p.Register != "" {
			msg += " (register it at " + p.Register + ")"
		}
		return Registration{}, log.Code("oauth_not_registered").Public(msg).New(msg)
	}
	return r, nil
}

// Start begins a sign-in for a connection the principal manages: a
// single-use state, and the provider's authorize URL to open.
func Start(args features.ServiceArgs, d Deps, id, returnTo string) (string, error) {
	cd := d.Connections()
	s, err := lookup(args, cd, id)
	if err != nil {
		return "", err
	}
	if args.Principal.Kind != features.PrincipalUser {
		return "", log.Code("oauth_person_only").Errorf("only a person signs in")
	}
	reg, err := registration(args, cd, s.provider)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(reg.RedirectBase, "/")
	if base == "" {
		base = strings.TrimRight(d.PublicBase, "/")
	}
	if base == "" {
		return "", log.Code("oauth_no_redirect").Errorf("the server has no public URL for %s to send you back to", s.provider.Title)
	}
	if returnTo != "" && (!strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//")) {
		returnTo = "" // only a route of this site
	}
	st := state{Connection: id, Tenant: args.Principal.TenantID, User: args.Principal.UserID, Provider: s.provider.ID,
		Verifier: random(32), Redirect: base + CallbackPath, Return: returnTo, At: d.now().UTC()}
	kv, err := statesKV(args.Context(), cd.Projections.JetStream(), st.Tenant)
	if err != nil {
		return "", log.Wrap(err)
	}
	key := stateKey()
	raw, _ := json.Marshal(st)
	if _, err := kv.Create(args.Context(), key, raw); err != nil {
		return "", log.Wrap(err)
	}
	return AuthorizeURL(s.provider, s.choice, reg, st.Redirect, key, st.Verifier), nil
}

// AuthorizeURL is where a sign-in sends the person: the provider's
// authorize endpoint with the registration's client, the redirect, the
// scopes (the provider's defaults, then the choice's), the state, the PKCE
// challenge and the provider's and choice's extra parameters.
func AuthorizeURL(p connectors.OAuthProvider, c connectors.OAuthChoice, reg Registration, redirect, state, verifier string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", reg.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("scope", strings.Join(p.ScopesFor(c), p.Separator()))
	q.Set("state", state)
	if p.PKCE {
		q.Set("code_challenge", challenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range p.Extra {
		q.Set(k, v)
	}
	for k, v := range c.Extra {
		q.Set(k, v)
	}
	u := p.Endpoint(p.AuthorizeURL, reg.Tenant)
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + q.Encode()
}

// Done is a finished sign-in.
type Done struct {
	Connection string `json:"connection"`
	Account    string `json:"account,omitempty"`
	Return     string `json:"return,omitempty"`
}

// Finish takes the provider's answer: it spends the state, which must be
// the principal's own, trades the code for tokens and keeps them on the
// connection. providerError is the provider's error parameter, if any.
func Finish(args features.ServiceArgs, d Deps, key, code, providerError string) (Done, error) {
	ctx, cd := args.Context(), d.Connections()
	kv, err := statesKV(ctx, cd.Projections.JetStream(), args.Principal.TenantID)
	if err != nil {
		return Done{}, log.Wrap(err)
	}
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrInvalidKey) {
		return Done{}, log.Code("oauth_state_unknown").Errorf("this sign-in is unknown, finished or expired: start it again")
	}
	if err != nil {
		return Done{}, log.Wrap(err)
	}
	var st state
	if err := json.Unmarshal(e.Value(), &st); err != nil {
		return Done{}, log.Wrap(err)
	}
	if args.Principal.Kind != features.PrincipalUser || st.User != args.Principal.UserID || st.Tenant != args.Principal.TenantID {
		return Done{}, log.Code("oauth_state_other").Errorf("someone else started this sign-in")
	}
	// Spent at the revision read: a second tab finishing it too fails here
	if err := kv.Delete(ctx, key, jetstream.LastRevision(e.Revision())); err != nil {
		return Done{}, log.Code("oauth_state_unknown").Errorf("this sign-in was already finished: start it again")
	}
	done := Done{Connection: st.Connection, Return: st.Return}
	if providerError != "" {
		return done, log.Code("oauth_denied").Errorf("the sign-in was refused: %s", providerError)
	}
	s, err := lookup(args, cd, st.Connection)
	if err != nil {
		return done, err
	}
	if s.provider.ID != st.Provider {
		return done, log.Code("oauth_provider_changed").Errorf("connection %s's provider changed: sign in again", s.c.Name)
	}
	reg, err := registration(args, cd, s.provider)
	if err != nil {
		return done, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {st.Redirect}}
	if s.provider.PKCE {
		form.Set("code_verifier", st.Verifier)
	}
	t, raw, terr := d.token(ctx, s.provider, reg, form)
	if terr != nil {
		return done, log.Code("oauth_exchange_failed").Errorf("%s did not give the tokens: %s", s.provider.Title, terr.Code)
	}
	account, err := d.account(ctx, s.provider, t, raw)
	if err != nil {
		return done, log.Code("oauth_account").Errorf("%s did not say who signed in", s.provider.Title)
	}
	done.Account = account
	if _, err := keep(args, cd, st.Connection, t, &account); err != nil {
		return done, err
	}
	return done, nil
}

// tokenError is a token endpoint's error.
type tokenError struct {
	Code   string // invalid_grant, invalid_client, … or http_<status>
	Status int
}

// token posts to the provider's token endpoint with the registration's
// client credentials.
func (d Deps) token(ctx context.Context, p connectors.OAuthProvider, reg Registration, form url.Values) (connections.Tokens, map[string]any, *tokenError) {
	req := TokenRequest(ctx, p, reg, form)
	res, err := d.client().Do(req)
	if err != nil {
		return connections.Tokens{}, nil, &tokenError{Code: "unreachable"}
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 1<<20)).Decode(&body)
	if e, _ := body["error"].(string); e != "" {
		return connections.Tokens{}, nil, &tokenError{Code: e, Status: res.StatusCode}
	}
	if res.StatusCode >= 300 {
		return connections.Tokens{}, nil, &tokenError{Code: "http_" + strconv.Itoa(res.StatusCode), Status: res.StatusCode}
	}
	t := connections.Tokens{Provider: p.ID}
	t.AccessToken, _ = body["access_token"].(string)
	t.RefreshToken, _ = body["refresh_token"].(string)
	t.TokenType, _ = body["token_type"].(string)
	t.Scope, _ = body["scope"].(string)
	if t.AccessToken == "" {
		return connections.Tokens{}, nil, &tokenError{Code: "no_access_token", Status: res.StatusCode}
	}
	switch v := body["expires_in"].(type) {
	case float64:
		t.ExpiresAt = d.now().Add(time.Duration(v) * time.Second).UTC()
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			t.ExpiresAt = d.now().Add(time.Duration(n) * time.Second).UTC()
		}
	}
	return t, body, nil
}

// Exchange posts a grant to the provider's token endpoint: the tokens, or
// the provider's error code (invalid_grant, …, http_<status>,
// unreachable, no_access_token).
func (d Deps) Exchange(ctx context.Context, p connectors.OAuthProvider, reg Registration, form url.Values) (connections.Tokens, string) {
	t, _, err := d.token(ctx, p, reg, form)
	if err != nil {
		return connections.Tokens{}, err.Code
	}
	return t, ""
}

// TokenRequest is a request to the provider's token endpoint with the
// registration's client credentials, in the form or as HTTP Basic
// (OAuthProvider.TokenAuth).
func TokenRequest(ctx context.Context, p connectors.OAuthProvider, reg Registration, form url.Values) *http.Request {
	basic := p.TokenAuth == "basic" && reg.ClientSecret != ""
	if !basic {
		form.Set("client_id", reg.ClientID)
		if reg.ClientSecret != "" {
			form.Set("client_secret", reg.ClientSecret)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint(p.TokenURL, reg.Tenant), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(url.QueryEscape(reg.ClientID), url.QueryEscape(reg.ClientSecret))
	}
	return req
}

// account reads who signed in.
func (d Deps) account(ctx context.Context, p connectors.OAuthProvider, t connections.Tokens, token map[string]any) (string, error) {
	from := token
	if p.UserInfoURL != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.UserInfoURL, nil)
		req.Header.Set("Authorization", "Bearer "+t.AccessToken)
		req.Header.Set("Accept", "application/json")
		res, err := d.client().Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return "", fmt.Errorf("userinfo: %d", res.StatusCode)
		}
		from = map[string]any{}
		if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 1<<20)).Decode(&from); err != nil {
			return "", err
		}
	}
	if p.UserInfoURL != "" {
		// id_token claims still come from the token response
		if idt, ok := token["id_token"]; ok {
			from["id_token"] = idt
		}
	}
	return ReadAccount(p.AccountField, from)
}

// ReadAccount reads an AccountField from a token response or userinfo
// answer: a dotted path (team.id), a claim of its id_token
// (id_token.email; the token came straight from the provider over TLS, so
// its signature is not checked), or a template of those in braces
// ({team.id}/{authed_user.id}).
func ReadAccount(field string, from map[string]any) (string, error) {
	if strings.Contains(field, "{") {
		var out strings.Builder
		rest := field
		for {
			i := strings.Index(rest, "{")
			if i < 0 {
				out.WriteString(rest)
				break
			}
			j := strings.Index(rest[i:], "}")
			if j < 0 {
				return "", fmt.Errorf("unclosed { in %s", field)
			}
			out.WriteString(rest[:i])
			v, err := ReadAccount(rest[i+1:i+j], from)
			if err != nil {
				return "", err
			}
			out.WriteString(v)
			rest = rest[i+j+1:]
		}
		return out.String(), nil
	}
	cur := any(from)
	parts := strings.Split(field, ".")
	for i, part := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("no %s", field)
		}
		cur = m[part]
		if part == "id_token" && i == 0 && len(parts) > 1 {
			raw, _ := cur.(string)
			claims, err := jwtClaims(raw)
			if err != nil {
				return "", err
			}
			cur = claims
		}
	}
	switch v := cur.(type) {
	case string:
		if v != "" {
			return v, nil
		}
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("no %s", field)
}

// jwtClaims are a JWT's payload, unverified.
func jwtClaims(raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("the id_token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(b, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// keep commits tokens to a connection as args' principal, and waits until
// the projection holds them.
func keep(args features.ServiceArgs, cd connections.Deps, id string, t connections.Tokens, account *string) (uint64, error) {
	raw, _ := json.Marshal(t)
	a := map[string]any{"secrets": map[string]any{connections.OAuthSecret: string(raw)}}
	if account != nil {
		a["account"] = *account
	}
	body, _ := json.Marshal(a)
	res := cd.Bus.Execute(bus.Request{ServiceArgs: args, ID: "oauth-" + strconv.FormatUint(features.NewTSID(), 10), Type: "connection.setTokens", Target: id, Args: body})
	if res.Common.Code >= 300 {
		return 0, log.Code("oauth_not_kept").Errorf("the sign-in could not be kept: %s", res.Common.Message)
	}
	if _, err := cd.Projections.WaitFor(args.Context(), args.Principal.TenantID, connections.Projector, connections.TargetType+"."+id, res.Seq, 10*time.Second); err != nil {
		return 0, log.Wrap(err)
	}
	return res.Seq, nil
}
