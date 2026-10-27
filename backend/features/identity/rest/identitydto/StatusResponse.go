package identitydto

import (
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type StatusResponse struct {
	rest.Status
	SingleUser bool     `json:"singleUser"`
	SignedIn   bool     `json:"signedIn"`
	Account    *Account `json:"account,omitempty"`
}

func StatusResponseFrom(resp identityservice.StatusResponse) StatusResponse {
	out := StatusResponse{Status: rest.StatusFrom(resp.Common), SingleUser: resp.SingleUser, SignedIn: resp.SignedIn}
	if resp.Account != nil {
		a := AccountFrom(*resp.Account)
		out.Account = &a
	}
	return out
}
