package connectors

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// OAuth sign-in for connector types (forge 26a-1, documentation/
// Connectors.md › Built in forge 26a-1): a type names a provider and the
// scopes it needs; the provider says where to send the person, where to
// trade the code for tokens and how to learn who signed in.

// OAuth is a type's sign-in: Type.OAuth. A type signs in with one
// provider (Provider, Scopes), or lets each connection choose one of
// Choices in its settings field Field (forge 26a-2: Mail's SMTP and POP3
// sign in to Google or Microsoft).
type OAuth struct {
	// Provider is an OAuthProviders contribution's id (google).
	Provider string `json:"provider,omitempty"`
	// Scopes are what the type asks for, each one the provider declares.
	Scopes []string `json:"scopes,omitempty"`
	// Extra are more authorize URL parameters (access_type=offline).
	Extra map[string]string `json:"extra,omitempty"`
	// Field is the settings field naming the chosen provider, one of
	// Choices; a connection that leaves it empty does not sign in.
	Field string `json:"field,omitempty"`
	// Choices are the providers a connection may choose, each with its
	// scopes.
	Choices []OAuthChoice `json:"choices,omitempty"`
}

// OAuthChoice is one provider a connection may choose.
type OAuthChoice struct {
	Provider string            `json:"provider"`
	Scopes   []string          `json:"scopes"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// For is the sign-in a connection with these settings uses: the type's
// own provider, or the choice its Field names. ok is false when the
// connection chose none (it signs in another way, with a password).
func (o OAuth) For(settings json.RawMessage) (OAuthChoice, bool) {
	if o.Field == "" {
		return OAuthChoice{Provider: o.Provider, Scopes: o.Scopes, Extra: o.Extra}, o.Provider != ""
	}
	var st map[string]any
	_ = json.Unmarshal(settings, &st)
	chosen, _ := st[o.Field].(string)
	for _, c := range o.Choices {
		if c.Provider == chosen {
			return c, true
		}
	}
	return OAuthChoice{}, false
}

// Providers are every provider the sign-in may use.
func (o OAuth) Providers() []string {
	if o.Field == "" {
		return []string{o.Provider}
	}
	var out []string
	for _, c := range o.Choices {
		out = append(out, c.Provider)
	}
	return out
}

// OAuthProvider is an authorization server.
type OAuthProvider struct {
	ID           string `json:"id"`
	Title        string `json:"title"` // Sign in with <Title>
	AuthorizeURL string `json:"authorizeURL"`
	TokenURL     string `json:"tokenURL"`
	RevokeURL    string `json:"revokeURL,omitempty"`
	// PKCE sends an S256 code challenge (every provider should).
	PKCE bool `json:"pkce"`
	// ScopeSeparator joins scopes in the authorize URL (" " when empty).
	ScopeSeparator string `json:"scopeSeparator,omitempty"`
	// Scopes are the scopes a type may ask for, each with what it allows.
	Scopes map[string]string `json:"scopes"`
	// The signed-in account: UserInfoURL's AccountField, read with the new
	// access token, else AccountField of the token response.
	UserInfoURL  string `json:"userInfoURL,omitempty"`
	AccountField string `json:"accountField,omitempty"`
	// Register says where an administrator registers the app, for the
	// connection form when the tenant has no registration.
	Register string `json:"register,omitempty"`
	// Extra are authorize URL parameters every sign-in sends (Google's
	// access_type=offline and prompt=consent, for a refresh token).
	Extra map[string]string `json:"extra,omitempty"`
	// DefaultScopes are asked for on every sign-in, before the type's
	// (openid for the account claim, offline_access for a refresh token);
	// each is one of Scopes.
	DefaultScopes []string `json:"defaultScopes,omitempty"`
	// TenantSegment: the URLs hold {tenant}, filled from the
	// registration's tenant (Microsoft's common, organizations or a
	// directory id; common when empty).
	TenantSegment bool `json:"tenantSegment,omitempty"`
	// TokenAuth is how the client authenticates at the token endpoint:
	// "post" (client_id and client_secret in the form, the default) or
	// "basic" (HTTP Basic).
	TokenAuth string `json:"tokenAuth,omitempty"`
}

// AccountField forms (forge 26a-2): a dotted path into the token response
// or the userinfo answer (team.id), a claim of the token response's
// id_token (id_token.email), or a template of either in braces
// ({team.id}/{authed_user.id}).

// ScopesFor are the scopes a sign-in asks for: the provider's defaults,
// then the choice's, each once.
func (p OAuthProvider) ScopesFor(c OAuthChoice) []string {
	var out []string
	for _, s := range append(slices.Clone(p.DefaultScopes), c.Scopes...) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Endpoint is one of the provider's URLs with {tenant} filled.
func (p OAuthProvider) Endpoint(u, tenant string) string {
	if !p.TenantSegment {
		return u
	}
	if tenant == "" {
		tenant = "common"
	}
	return strings.ReplaceAll(u, "{tenant}", url.PathEscape(tenant))
}

// BuiltinOAuthProvider is a connector.oauthProvider contribution; its
// Descriptor's ID is the provider's.
type BuiltinOAuthProvider struct {
	extensions.Descriptor
	Provider OAuthProvider
}

// OAuthProviders is the connector.oauthProvider point.
var OAuthProviders = extensions.NewPoint[BuiltinOAuthProvider]("connector.oauthProvider",
	"OAuth authorization servers connector types sign in with: authorize, token and revoke URLs, PKCE, scopes and how the account is read (forge 26a-1)")

// ContributeOAuthProvider registers a provider.
func ContributeOAuthProvider(reg *extensions.Registry, source string, p OAuthProvider) {
	OAuthProviders.Register(reg, BuiltinOAuthProvider{Descriptor: extensions.Descriptor{ID: p.ID, Source: source}, Provider: p})
}

// OAuthProviderOf finds a provider.
func OAuthProviderOf(reg *extensions.Registry, id string) (OAuthProvider, bool) {
	for _, p := range OAuthProviders.All(reg) {
		if p.Provider.ID == id {
			return p.Provider, true
		}
	}
	return OAuthProvider{}, false
}

// Separator is how scopes are joined.
func (p OAuthProvider) Separator() string {
	if p.ScopeSeparator == "" {
		return " "
	}
	return p.ScopeSeparator
}

// CheckOAuthProvider checks a provider's metadata.
func CheckOAuthProvider(p OAuthProvider) []Problem {
	c := &checker{}
	if !ValidID(p.ID) || strings.Contains(p.ID, ".") {
		c.add("id", "%q is not a provider id: one word of letters, digits and -", p.ID)
	}
	if strings.TrimSpace(p.Title) == "" {
		c.add("title", "a provider needs a title (Sign in with …)")
	}
	for field, u := range map[string]string{"authorizeURL": p.AuthorizeURL, "tokenURL": p.TokenURL, "revokeURL": p.RevokeURL, "userInfoURL": p.UserInfoURL} {
		if u == "" && (field == "revokeURL" || field == "userInfoURL") {
			continue
		}
		if strings.Contains(u, "{tenant}") && !p.TenantSegment {
			c.add(field, "%q holds {tenant}, but the provider has no tenantSegment", u)
		}
		if pu, err := url.Parse(p.Endpoint(u, "common")); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
			c.add(field, "%q is not a URL", u)
		}
	}
	for _, s := range p.DefaultScopes {
		if _, ok := p.Scopes[s]; !ok {
			c.add("defaultScopes", "%q is not one of the provider's scopes", s)
		}
	}
	if p.TokenAuth != "" && p.TokenAuth != "post" && p.TokenAuth != "basic" {
		c.add("tokenAuth", "%q is not post or basic", p.TokenAuth)
	}
	if len(p.Scopes) == 0 {
		c.add("scopes", "a provider declares the scopes types may ask for")
	}
	for s, about := range p.Scopes {
		if strings.TrimSpace(about) == "" {
			c.add("scopes."+s, "describe what the scope allows")
		}
	}
	if p.AccountField == "" {
		c.add("accountField", "say which field names the signed-in account")
	}
	return c.out
}

// CheckOAuth checks a type's sign-in against its provider.
func CheckOAuth(t Type, provider func(id string) (OAuthProvider, bool)) []Problem {
	c := &checker{}
	if t.OAuth == nil {
		return nil
	}
	if t.NoConnection {
		c.add("oauth", "a type that needs no connection signs nobody in")
	}
	o := *t.OAuth
	choices := []struct {
		path string
		c    OAuthChoice
	}{}
	if o.Field == "" {
		if len(o.Choices) > 0 {
			c.add("oauth.choices", "choices need the settings field that names the chosen one")
		}
		choices = append(choices, struct {
			path string
			c    OAuthChoice
		}{"oauth", OAuthChoice{Provider: o.Provider, Scopes: o.Scopes, Extra: o.Extra}})
	} else {
		if o.Provider != "" || len(o.Scopes) > 0 {
			c.add("oauth.provider", "a type with choices names its providers in them, not in provider and scopes")
		}
		if len(o.Choices) == 0 {
			c.add("oauth.choices", "name the providers a connection may choose")
		}
		var st struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(t.Settings, &st)
		if _, ok := st.Properties[o.Field]; !ok {
			c.add("oauth.field", "the settings have no field %q to choose the provider in", o.Field)
		}
		for i, ch := range o.Choices {
			choices = append(choices, struct {
				path string
				c    OAuthChoice
			}{fmt.Sprintf("oauth.choices.%d", i), ch})
		}
	}
	for _, ch := range choices {
		p, ok := provider(ch.c.Provider)
		if !ok {
			c.add(ch.path+".provider", "%q is not an OAuth provider", ch.c.Provider)
			continue
		}
		if len(ch.c.Scopes) == 0 {
			c.add(ch.path+".scopes", "name the scopes the type needs")
		}
		for _, s := range ch.c.Scopes {
			if _, ok := p.Scopes[s]; !ok {
				known := make([]string, 0, len(p.Scopes))
				for k := range p.Scopes {
					known = append(known, k)
				}
				slices.Sort(known)
				c.add(ch.path+".scopes", "%s does not declare the scope %q: use one of %s", p.ID, s, strings.Join(known, ", "))
			}
		}
	}
	return c.out
}
