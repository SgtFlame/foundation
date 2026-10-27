package identitydto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// SessionResponse is the response of every operation that starts or
// renews a session: the tokens travel as cookies, so the body has only the
// access token's expiry and, when it changed, the account.
type SessionResponse struct {
	rest.Status
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	Account   *Account  `json:"account,omitempty"`
}

// SessionResponseFrom converts what a session operation returned; account
// is nil when it did not change.
func SessionResponseFrom(common features.ServiceResponse, session identity.Session, account *Account) SessionResponse {
	return SessionResponse{Status: rest.StatusFrom(common), ExpiresAt: session.AccessExpires, Account: account}
}
