package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type DeleteResponse struct {
	rest.Status
}

// DeleteResponseFrom converts the service's DeleteResponse; its code is
// carried by the HTTP status rather than the body.
func DeleteResponseFrom(resp secretsservice.DeleteResponse) DeleteResponse {
	return DeleteResponse{Status: rest.StatusFrom(resp.Common)}
}
