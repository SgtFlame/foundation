package secretsdto

import (
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

type ListResponse struct {
	rest.Status
	Secrets []Secret `json:"secrets"`
}

// ListResponseFrom converts the service's ListResponse; its code is carried
// by the HTTP status rather than the body.
func ListResponseFrom(resp secretsservice.ListResponse) ListResponse {
	out := ListResponse{Status: rest.StatusFrom(resp.Common), Secrets: []Secret{}}
	for _, s := range resp.Secrets {
		out.Secrets = append(out.Secrets, SecretFrom(s))
	}
	return out
}
