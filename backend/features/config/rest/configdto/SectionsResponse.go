package configdto

import (
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type SectionsResponse struct {
	rest.Status
	Sections []SectionInfo `json:"sections,omitempty"`
}

// SectionsResponseFrom converts the service's SectionsResponse; its code is
// carried by the HTTP status rather than the body.
func SectionsResponseFrom(resp configservice.SectionsResponse) SectionsResponse {
	out := SectionsResponse{Status: rest.StatusFrom(resp.Common)}
	for _, s := range resp.Sections {
		out.Sections = append(out.Sections, SectionInfoFrom(s))
	}
	return out
}
