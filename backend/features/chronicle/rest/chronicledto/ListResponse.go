package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListResponse struct {
	rest.Status
	Entries []Entry `json:"entries"`
	Links   []Link  `json:"links"`
}

// ListResponseFrom converts the service's ListResponse; its code is carried
// by the HTTP status rather than the body.
func ListResponseFrom(resp chronicleservice.ListResponse) ListResponse {
	out := ListResponse{Status: rest.StatusFrom(resp.Common), Entries: make([]Entry, len(resp.Entries)), Links: linksFrom(resp.Links)}
	for i, e := range resp.Entries {
		out.Entries[i] = EntryFrom(e, false)
	}
	return out
}
