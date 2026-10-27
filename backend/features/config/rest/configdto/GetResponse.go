package configdto

import (
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type GetResponse struct {
	rest.Status
	View
}

// GetResponseFrom converts the service's GetResponse; its code is carried by
// the HTTP status rather than the body.
func GetResponseFrom(resp configservice.GetResponse) GetResponse {
	return GetResponse{Status: rest.StatusFrom(resp.Common), View: ViewFrom(resp.View)}
}
