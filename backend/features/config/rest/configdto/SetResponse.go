package configdto

import (
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type SetResponse struct {
	rest.Status
	View
	Errors []FieldError `json:"errors,omitempty"`
}

// SetResponseFrom converts the service's SetResponse; its code is carried by
// the HTTP status rather than the body.
func SetResponseFrom(resp configservice.SetResponse) SetResponse {
	out := SetResponse{Status: rest.StatusFrom(resp.Common), View: ViewFrom(resp.View)}
	for _, e := range resp.Errors {
		out.Errors = append(out.Errors, FieldError{Path: e.Path, Message: e.Message})
	}
	return out
}
