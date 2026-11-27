package connections_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/SgtFlame/foundation/backend/features/connectors/oauth"
	"github.com/SgtFlame/foundation/backend/features/connectors/oauth/oauthtest"
	"github.com/samber/oops"
)

// registrations is the connectors.oauth section as configured.
type registrations struct{}

func (registrations) Resolved(features.ServiceArgs, string) (json.RawMessage, error) {
	return json.Marshal(map[string]oauth.Registration{oauthtest.ProviderID: {ClientID: oauthtest.ClientID, ClientSecret: oauthtest.ClientSecret}})
}

func codeOf(err error) string {
	if o, ok := oops.AsOops(err); ok {
		s, _ := o.Code().(string)
		return s
	}
	return ""
}

func oauthSetup(t *testing.T) (*fixture, *oauthtest.Server, oauth.Deps) {
	f := setup(t)
	prov := oauthtest.New(t)
	connectors.ContributeOAuthProvider(f.reg, "test", prov.Provider())
	connectors.ContributeBuiltins(f.reg, "test", nil, []connectors.Type{oauthtest.Type(connectorstest.ContractRef)})
	oauth.ContributeSettings(f.reg)
	od := oauth.Deps{Connections: func() connections.Deps { return *f.live }, PublicBase: "http://axiom.test/"}
	f.live.Config, f.live.Extensions = registrations{}, f.reg
	f.live.Tokens = oauth.Tokens{Deps: od, Holder: "p1"}
	f.deps = *f.live
	return f, prov, od
}

// signIn runs a whole sign-in as user 1.
func signIn(t *testing.T, prov *oauthtest.Server, od oauth.Deps, id string) oauth.Done {
	t.Helper()
	u, err := oauth.Start(args(), od, id, "")
	if err != nil {
		t.Fatal(err)
	}
	_, q, err := prov.Consent(u)
	if err != nil {
		t.Fatal(err)
	}
	done, err := oauth.Finish(args(), od, q.Get("state"), q.Get("code"), "")
	if err != nil {
		t.Fatal(err)
	}
	return done
}

// An OAuth sign-in keeps its tokens as the connection's secret, shown only
// as set; its state is single use and the starter's alone; a refused PKCE
// verifier fails it; cx.resolve gives the access token, never the refresh
// token (forge 26a-1).
func TestOAuthSignIn(t *testing.T) {
	f, prov, od := oauthSetup(t)
	c := f.create(map[string]any{"type": "test.oauth", "name": "Ann's", "settings": map[string]any{"label": "a"}})
	u, err := oauth.Start(args(), od, c.ID, "/workbench?app=mail")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "access_type=offline") || !strings.Contains(u, "scope=read") {
		t.Fatalf("authorize URL: %s", u)
	}
	redirect, q, err := prov.Consent(u)
	if err != nil {
		t.Fatal(err)
	}
	if redirect != "http://axiom.test"+oauth.CallbackPath {
		t.Fatalf("redirect: %s", redirect)
	}
	if _, err := oauth.Finish(user(2), od, q.Get("state"), q.Get("code"), ""); codeOf(err) != "oauth_state_other" {
		t.Fatalf("another person finished it: %v", err)
	}
	done, err := oauth.Finish(args(), od, q.Get("state"), q.Get("code"), "")
	if err != nil {
		t.Fatal(err)
	}
	if done.Account != "ann@example.com" || done.Return != "/workbench?app=mail" {
		t.Fatalf("done: %+v", done)
	}
	if _, err := oauth.Finish(args(), od, q.Get("state"), q.Get("code"), ""); codeOf(err) != "oauth_state_unknown" {
		t.Fatalf("a spent state: %v", err)
	}
	if _, err := oauth.Finish(args(), od, "0123456789abcdef", "x", ""); codeOf(err) != "oauth_state_unknown" {
		t.Fatalf("an unknown state: %v", err)
	}
	desc := f.query(args(), "connections.describe", map[string]any{"id": c.ID})
	eventually(t, "the account shown", 3*time.Second, func() bool { return strings.Contains(desc.json(), `"account":"ann@example.com"`) })
	if j := desc.json(); !strings.Contains(j, `"oauth":{"set":true}`) || !strings.Contains(j, `"title":"Test provider"`) || strings.Contains(j, "at-") || strings.Contains(j, "rt-") {
		t.Fatalf("describe: %s", j)
	}
	conn, err := adapter.ResolveOverBus(context.Background(), f.srv.Conn(), 1, "test.oauth", c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	_ = json.Unmarshal(conn.Settings, &s)
	if tok, _ := s["accessToken"].(string); !strings.HasPrefix(tok, "at-") || s["expiresAt"] == nil || s["oauth"] != nil || strings.Contains(string(conn.Settings), "rt-") {
		t.Fatalf("resolved settings: %s", conn.Settings)
	}
	// A refused verifier: the challenge the provider holds is another's
	u, _ = oauth.Start(args(), od, c.ID, "")
	_, q, _ = prov.Consent(strings.Replace(u, "code_challenge=", "code_challenge=x", 1))
	if _, err := oauth.Finish(args(), od, q.Get("state"), q.Get("code"), ""); codeOf(err) != "oauth_exchange_failed" {
		t.Fatalf("a refused verifier: %v", err)
	}
	// The person refusing on the provider's page
	u, _ = oauth.Start(args(), od, c.ID, "")
	_, q, _ = prov.Consent(u)
	if _, err := oauth.Finish(args(), od, q.Get("state"), "", "access_denied"); codeOf(err) != "oauth_denied" {
		t.Fatalf("refused: %v", err)
	}
	// Someone who may not manage it cannot start one
	if _, err := oauth.Start(user(2), od, c.ID, ""); err == nil {
		t.Fatal("another person started a sign-in on Ann's connection")
	}
}

// Two processes resolving an expiring token refresh it once and both get
// the new one; a refused refresh token marks the connection to sign in
// again, and a new sign-in clears it.
func TestOAuthRefresh(t *testing.T) {
	f, prov, od := oauthSetup(t)
	c := f.create(map[string]any{"type": "test.oauth", "name": "Ann's", "settings": map[string]any{}})
	prov.ExpiresIn = time.Minute // within RefreshBefore: stale at once
	signIn(t, prov, od, c.ID)
	prov.ExpiresIn, prov.Delay = time.Hour, 200*time.Millisecond
	cur, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
	var wg sync.WaitGroup
	got := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := *f.live
			d.Tokens = oauth.Tokens{Deps: od, Holder: []string{"p1", "p2"}[i]}
			raw, err := connections.Settings(context.Background(), d, 1, cur)
			var s map[string]any
			_ = json.Unmarshal(raw, &s)
			got[i], _ = s["accessToken"].(string)
			errs[i] = err
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || got[0] == "" || got[0] != got[1] {
		t.Fatalf("tokens %v, errors %v", got, errs)
	}
	if n := prov.Refreshes.Load(); n != 1 {
		t.Fatalf("refreshes: %d", n)
	}
	// Access withdrawn at the provider
	prov.Delay, prov.ExpiresIn = 0, time.Minute
	signIn(t, prov, od, c.ID)
	prov.Invalidate()
	cur, _, _ = connections.Get(context.Background(), f.p, 1, c.ID)
	_, err := connections.Settings(context.Background(), *f.live, 1, cur)
	if e, ok := err.(*connectors.Error); !ok || e.Code != connectors.CodeAuth {
		t.Fatalf("an invalid grant: %v", err)
	}
	eventually(t, "the connection marked", 3*time.Second, func() bool {
		now, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
		return now.Status == connections.StatusError && now.LastError == connections.SignInAgain
	})
	prov.ExpiresIn = time.Hour
	signIn(t, prov, od, c.ID)
	now, _, _ := connections.Get(context.Background(), f.p, 1, c.ID)
	if now.Status != connections.StatusOK {
		t.Fatalf("after signing in again: %s %s", now.Status, now.LastError)
	}
}
