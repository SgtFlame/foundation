package connections

import (
	"context"
	"encoding/json"
	"time"
)

// Tokens are what an OAuth sign-in gave a connection (forge 26a-1), kept
// as its oauth secret. Only AccessToken and ExpiresAt ever reach an
// adapter; the refresh token stays with Foundation.
type Tokens struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	TokenType    string    `json:"tokenType,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
	// Provider is the provider that issued them.
	Provider string `json:"provider"`
}

// SignInAgain is the last error of a connection whose refresh token was
// refused: the form shows Sign in again (forge 26a-1).
const SignInAgain = "signIn: the sign-in expired; sign in again"

// TokenSource gives an adapter a connection's access token, refreshing
// it when it is about to expire (connectors/oauth.Tokens).
type TokenSource interface {
	Access(ctx context.Context, d Deps, tenant uint64, c Connection) (Tokens, error)
}

// ReadTokens reads a connection's tokens and the version they are kept
// under; ok is false when it was never signed in.
func ReadTokens(ctx context.Context, d Deps, tenant uint64, c Connection) (t Tokens, version string, ok bool, err error) {
	version, ok = c.Secrets[OAuthSecret]
	if !ok {
		return Tokens{}, "", false, nil
	}
	raw, err := secretValue(ctx, d, tenant, c.ID, OAuthSecret, version)
	if err != nil {
		return Tokens{}, "", false, log.Wrapf(err, "the tokens of connection %s cannot be read", c.ID)
	}
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return Tokens{}, "", false, log.Wrapf(err, "the tokens of connection %s are not readable", c.ID)
	}
	return t, version, true, nil
}

// accessSettings are the settings an adapter gets of a connection's
// tokens.
func accessSettings(t Tokens) map[string]any {
	out := map[string]any{"accessToken": t.AccessToken}
	if !t.ExpiresAt.IsZero() {
		out["expiresAt"] = t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}
