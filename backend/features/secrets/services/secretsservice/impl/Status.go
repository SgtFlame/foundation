package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

// Status is the bootstrap state with the Vault's address and the token's
// expiry, for the Secrets editor.
func (s *service) Status(args secretsservice.StatusArgs) secretsservice.StatusResponse {
	if f := check(args.ServiceArgs, "secret.list"); f != nil {
		return secretsservice.StatusResponse{Common: *f}
	}
	return secretsservice.StatusResponse{Common: features.OK(), Status: s.boot.Status()}
}
