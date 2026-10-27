package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/secrets"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

var log = logging.In("secrets").Tags("bootstrap")

// Unseal spends a wrapped SecretID. It needs no session: only someone who
// may make SecretIDs for the server's AppRole in Vault can make a token it
// accepts, and each token works once.
func (s *service) Unseal(args bootstrapservice.UnsealArgs) bootstrapservice.UnsealResponse {
	err := s.boot.Unseal(args.Context(), args.Token, args.RoleID)
	if err != nil {
		status, err := uint16(http.StatusInternalServerError), log.Wrap(err)
		var refused *secrets.UnsealError
		if errors.As(err, &refused) {
			status, err = uint16(refused.Status), refused.Err
		}
		return bootstrapservice.UnsealResponse{Common: *args.Fail(status, err), State: s.boot.State()}
	}
	return bootstrapservice.UnsealResponse{Common: features.OK(), State: s.boot.State()}
}
