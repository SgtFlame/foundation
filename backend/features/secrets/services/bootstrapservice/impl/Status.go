package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/bootstrapservice"
)

// Status is the bootstrap state. It needs no session and no check: the
// browser reads it to show the sealed page, and unseal.sh to wait for ready.
func (s *service) Status(args bootstrapservice.StatusArgs) bootstrapservice.StatusResponse {
	return bootstrapservice.StatusResponse{Common: features.OK(), State: s.boot.State()}
}
