package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type SaveTypeResponse struct {
	rest.Status
	Type     *EntryType     `json:"type,omitempty"`
	Problems []live.Problem `json:"problems,omitempty"` // why it was refused, at paths like fields.<id>.type
}

// SaveTypeResponseFrom converts the service's SaveTypeResponse.
func SaveTypeResponseFrom(resp chronicleservice.SaveTypeResponse) SaveTypeResponse {
	out := SaveTypeResponse{Status: rest.StatusFrom(resp.Common), Problems: resp.Problems}
	if resp.Common.Code < 300 {
		t := EntryTypeFrom(resp.Type)
		out.Type = &t
	}
	return out
}
