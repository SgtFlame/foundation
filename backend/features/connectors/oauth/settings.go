package oauth

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// SectionID is the app registrations' section: one object per provider.
const SectionID = "connectors.oauth"

// Registration is a tenant's (or the app scope's default) app registered
// with a provider.
type Registration struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	// RedirectBase is the public URL the provider sends people back to
	// (the server's when empty); the redirect URL is it + CallbackPath.
	RedirectBase string `json:"redirectBase"`
}

// ContributeSettings registers the connectors.oauth section, with an
// object per provider registered so far: call it after every
// connectors.ContributeOAuthProvider.
func ContributeSettings(reg *extensions.Registry) {
	props := map[string]any{}
	for _, b := range connectors.OAuthProviders.All(reg) {
		p := b.Provider
		about := "The app registered with " + p.Title + " that connections sign in with."
		if p.Register != "" {
			about += " Register it at " + p.Register + "."
		}
		props[p.ID] = map[string]any{
			"type": "object", "title": p.Title, "description": about,
			"properties": map[string]any{
				"clientId":     map[string]any{"type": "string", "title": "Client id"},
				"clientSecret": map[string]any{"type": "secret", "title": "Client secret"},
				"redirectBase": map[string]any{"type": "string", "title": "Redirect base", "format": "url",
					"description": "The public URL people come back to, such as https://axiom.example.com; the server's when empty. Register <base>" + CallbackPath + " with the provider."},
			},
		}
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": props})
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: SectionID, Order: 906, Source: "connectors"},
		Title:       "OAuth apps",
		Description: "The apps registered with each OAuth provider, which connections sign in with. An application administrator's are every tenant's default.",
		Category:    []string{"System", "Connectors"},
		Scopes:      []string{"app", "tenant"},
		Schema:      schema,
		Defaults:    json.RawMessage(`{}`),
		Version:     1,
	})
}
