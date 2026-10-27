// Package bootstrapservice serves the sealed start
// (documentation/plans/Secrets.md › Sealed start): the state, which anyone
// may read, and unsealing with a response-wrapped SecretID. Its routes are
// the only ones a sealed server answers.
package bootstrapservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets"
)

type Service interface {
	Status(args StatusArgs) StatusResponse
	Unseal(args UnsealArgs) UnsealResponse
}

type StatusArgs struct {
	features.ServiceArgs
}

// StatusResponse is the state alone: sealed, starting, ready or degraded.
type StatusResponse struct {
	Common features.ServiceResponse
	State  secrets.State
}

// UnsealArgs carry a response-wrapped, single-use SecretID of the server's
// AppRole, and its RoleID unless serve was given --vault-role-id.
type UnsealArgs struct {
	features.ServiceArgs
	Token  string
	RoleID string
}

type UnsealResponse struct {
	Common features.ServiceResponse
	State  secrets.State
}
