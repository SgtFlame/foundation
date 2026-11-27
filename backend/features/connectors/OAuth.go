package connectors

import (
	"net/url"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// OAuth sign-in for connector types (forge 26a-1, documentation/
// Connectors.md › Built in forge 26a-1): a type names a provider and the
// scopes it needs; the provider says where to send the person, where to
// trade the code for tokens and how to learn who signed in.

// OAuth is a type's sign-in: Type.OAuth.
type OAuth struct {
	// Provider is an OAuthProviders contribution's id (google).
	Provider string `json:"provider"`
	// Scopes are what the type asks for, each one the provider declares.
	Scopes []string `json:"scopes"`
	// Extra are more authorize URL parameters (access_type=offline).
	Extra map[string]string `json:"extra,omitempty"`
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
		if pu, err := url.Parse(u); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
			c.add(field, "%q is not a URL", u)
		}
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
	p, ok := provider(t.OAuth.Provider)
	if !ok {
		c.add("oauth.provider", "%q is not an OAuth provider", t.OAuth.Provider)
		return c.out
	}
	if len(t.OAuth.Scopes) == 0 {
		c.add("oauth.scopes", "name the scopes the type needs")
	}
	for _, s := range t.OAuth.Scopes {
		if _, ok := p.Scopes[s]; !ok {
			known := make([]string, 0, len(p.Scopes))
			for k := range p.Scopes {
				known = append(known, k)
			}
			slices.Sort(known)
			c.add("oauth.scopes", "%s does not declare the scope %q: use one of %s", p.ID, s, strings.Join(known, ", "))
		}
	}
	return c.out
}
