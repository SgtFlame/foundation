package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// EntryResponse answers create and update with the stored entry.
type EntryResponse struct {
	rest.Status
	Entry *Entry `json:"entry,omitempty"`
}

// EntryResponseFrom converts the service's EntryResponse; its code is
// carried by the HTTP status rather than the body.
func EntryResponseFrom(resp chronicleservice.EntryResponse) EntryResponse {
	out := EntryResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Entry.ID != 0 {
		entry := EntryFrom(resp.Entry, true)
		out.Entry = &entry
	}
	return out
}
