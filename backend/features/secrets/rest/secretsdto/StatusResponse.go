package secretsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type StatusResponse struct {
	rest.Status
	State        string    `json:"state,omitempty"`
	Vault        bool      `json:"vault"`
	Addr         string    `json:"addr,omitempty"`
	Mount        string    `json:"mount,omitempty"`
	TokenExpires time.Time `json:"tokenExpires,omitzero"`
	Problem      string    `json:"problem,omitempty"`
}

// StatusResponseFrom converts the service's StatusResponse; its code is
// carried by the HTTP status rather than the body.
func StatusResponseFrom(resp secretsservice.StatusResponse) StatusResponse {
	s := resp.Status
	return StatusResponse{Status: rest.StatusFrom(resp.Common), State: string(s.State), Vault: s.Vault, Addr: s.Addr, Mount: s.Mount, TokenExpires: s.TokenExpires, Problem: s.Problem}
}
