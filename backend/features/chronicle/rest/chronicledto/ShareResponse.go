package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ShareResponse struct {
	rest.Status
	Entry *Entry `json:"entry,omitempty"`
	Share *Share `json:"share,omitempty"`
}

// ShareResponseFrom converts the service's ShareResponse; its code is
// carried by the HTTP status rather than the body.
func ShareResponseFrom(resp chronicleservice.ShareResponse) ShareResponse {
	out := ShareResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		e := EntryFrom(resp.Entry, false)
		out.Entry = &e
		if resp.Share != nil {
			s := ShareFrom(*resp.Share)
			out.Share = &s
		}
	}
	return out
}
