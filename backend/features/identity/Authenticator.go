package identity

import (
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
	"gorm.io/gorm"
)

// Cookie names and paths. Both cookies are HTTP-only and SameSite=Strict;
// the refresh token goes only to the identity routes.
const (
	AccessCookie  = "foundation_access"
	RefreshCookie = "foundation_refresh"
	AccessPath    = "/api"
	RefreshPath   = "/api/v1/identity"
)

// Authenticator finds a request's principal. While the application has no
// users it runs in single-user mode, as before identity existed
// (MultiTenant.md question 3): every request is features.LocalPrincipal.
// Once setup has made the first user, a request needs a valid access token
// and is otherwise anonymous.
type Authenticator struct {
	signer   *Signer
	hasUsers atomic.Bool
}

// NewAuthenticator reads from gdb whether any user exists yet.
func NewAuthenticator(gdb *gorm.DB, signer *Signer) (*Authenticator, error) {
	var n int64
	if err := gdb.Model(&User{}).Count(&n).Error; err != nil {
		return nil, err
	}
	a := &Authenticator{signer: signer}
	a.hasUsers.Store(n > 0)
	return a, nil
}

// SingleUser tells whether no user exists yet, so sign-in is not required.
func (a *Authenticator) SingleUser() bool { return !a.hasUsers.Load() }

// UsersExist ends single-user mode; setup calls it.
func (a *Authenticator) UsersExist() { a.hasUsers.Store(true) }

// Signer is the access tokens' signer.
func (a *Authenticator) Signer() *Signer { return a.signer }

// Authenticate is the principal of r, for rest.WithAuthenticate. Without a
// valid token it returns the anonymous principal and why: ErrNoSession, or
// ErrExpired so the browser knows to refresh.
func (a *Authenticator) Authenticate(r *http.Request) (features.Principal, error) {
	if a.SingleUser() {
		return features.LocalPrincipal(), nil
	}
	c, err := r.Cookie(AccessCookie)
	if err != nil || c.Value == "" {
		return features.AnonymousPrincipal(), ErrNoSession
	}
	p, err := a.signer.Verify(c.Value)
	if err != nil {
		if !errors.Is(err, ErrExpired) {
			err = ErrNoSession
		}
		return features.AnonymousPrincipal(), err
	}
	return p, nil
}

// ErrNoSession is a request without a valid access token.
var ErrNoSession = errors.New("identity: not signed in")
