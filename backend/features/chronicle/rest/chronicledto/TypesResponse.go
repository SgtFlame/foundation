package chronicledto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type TypesResponse struct {
	rest.Status
	Types []EntryType `json:"types"`
}

// EntryType is a kind of entry; fields is the schema of its own fields.
type EntryType struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Icon        string          `json:"icon"`
	Description string          `json:"description"`
	Fields      json.RawMessage `json:"fields"`
}

// TypesResponseFrom converts the service's TypesResponse; its code is
// carried by the HTTP status rather than the body.
func TypesResponseFrom(resp chronicleservice.TypesResponse) TypesResponse {
	out := TypesResponse{Status: rest.StatusFrom(resp.Common), Types: make([]EntryType, len(resp.Types))}
	for i, t := range resp.Types {
		out.Types[i] = EntryType{ID: t.ID, Title: t.Title, Icon: t.Icon, Description: t.Description, Fields: t.Fields}
	}
	return out
}
