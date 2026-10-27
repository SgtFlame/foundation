package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type PutResponse struct {
	rest.Status
	Secret *Secret `json:"secret,omitempty"`
}

// PutResponseFrom converts the service's PutResponse; its code is carried
// by the HTTP status rather than the body.
func PutResponseFrom(resp secretsservice.PutResponse) PutResponse {
	out := PutResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Secret.Name != "" {
		s := SecretFrom(resp.Secret)
		out.Secret = &s
	}
	return out
}
