package oauth

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
)

// Tokens is connections.Deps.Tokens: the access token cx.resolve gives,
// refreshed when it is within RefreshBefore of expiring. One process
// refreshes at a time (a lock in cx_locks_<tenant>, oauth.<connection>),
// and one that waited for the lock takes the tokens the other kept, so a
// rotating refresh token is spent once.
type Tokens struct {
	Deps Deps
	// Holder names this process in the lock.
	Holder string
}

// Access gives c's tokens, fresh.
func (s Tokens) Access(ctx context.Context, cd connections.Deps, tenant uint64, c connections.Connection) (connections.Tokens, error) {
	t, version, ok, err := connections.ReadTokens(ctx, cd, tenant, c)
	if err != nil {
		return connections.Tokens{}, err
	}
	if !ok {
		return connections.Tokens{}, connectors.Errorf(connectors.CodeAuth, "connection %s is not signed in", c.Name).NotRetryable()
	}
	if !s.stale(t) {
		return t, nil
	}
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := connectors.Lock(lctx, cd.Projections.JetStream(), tenant, "oauth."+c.ID, s.holder())
	if err != nil {
		return connections.Tokens{}, connectors.Errorf(connectors.CodeUnavailable, "the sign-in of connection %s is being refreshed; try again", c.Name)
	}
	defer release()
	// Another process may have refreshed while this one waited
	if now, ok, err := connections.Get(ctx, cd.Projections, tenant, c.ID); err == nil && ok && now.Secrets[connections.OAuthSecret] != version {
		if t, _, _, err = connections.ReadTokens(ctx, cd, tenant, now); err != nil {
			return connections.Tokens{}, err
		}
		if !s.stale(t) {
			return t, nil
		}
	}
	return s.refresh(ctx, cd, tenant, c, t)
}

func (s Tokens) holder() string {
	if s.Holder != "" {
		return s.Holder
	}
	return "oauth-" + strconv.FormatUint(features.NewTSID(), 10)
}

// stale reports whether t expires within RefreshBefore and can be
// refreshed.
func (s Tokens) stale(t connections.Tokens) bool {
	return t.RefreshToken != "" && !t.ExpiresAt.IsZero() && s.Deps.now().Add(RefreshBefore).After(t.ExpiresAt)
}

func (s Tokens) refresh(ctx context.Context, cd connections.Deps, tenant uint64, c connections.Connection, t connections.Tokens) (connections.Tokens, error) {
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
	p, ok := connectors.OAuthProviderOf(cd.Extensions, t.Provider)
	if !ok {
		return connections.Tokens{}, connectors.Errorf(connectors.CodeAuth, "connection %s's provider %s is gone", c.Name, t.Provider).NotRetryable()
	}
	reg, err := registration(args, cd, p)
	if err != nil {
		return connections.Tokens{}, connectors.Errorf(connectors.CodeAuth, "%s", err.Error()).NotRetryable()
	}
	next, _, terr := s.Deps.token(ctx, p, reg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}})
	if terr != nil {
		if terr.Code == "invalid_grant" {
			connections.SetStatus(ctx, cd, tenant, c.ID, connections.StatusError, connections.SignInAgain)
			return connections.Tokens{}, connectors.Errorf(connectors.CodeAuth, "connection %s must be signed in again", c.Name).NotRetryable()
		}
		log.With("connection", c.ID).Warn(ctx, "%s did not refresh connection %s: %s", p.Title, c.ID, terr.Code)
		return connections.Tokens{}, connectors.Errorf(connectors.CodeUnavailable, "%s did not refresh the sign-in; try again", p.Title)
	}
	if next.RefreshToken == "" {
		next.RefreshToken = t.RefreshToken // not rotated
	}
	if _, err := keep(args, cd, c.ID, next, nil); err != nil {
		return connections.Tokens{}, connectors.Errorf(connectors.CodeUnavailable, "the refreshed sign-in could not be kept; try again")
	}
	return next, nil
}
