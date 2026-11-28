package chronicledto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type TypesResponse struct {
	rest.Status
	Types    []EntryType `json:"types"`
	Starters []Starter   `json:"starters"`
}

// EntryType is a kind of entry; fields is the schema of its own fields.
// A tenant's or project's type (custom) also has recordFields, in the
// records' field catalog, parentTypes and its scope.
type EntryType struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Icon         string          `json:"icon"`
	Description  string          `json:"description"`
	Fields       json.RawMessage `json:"fields"`
	RecordFields []records.Field `json:"recordFields,omitempty"`
	ParentTypes  []string        `json:"parentTypes,omitempty"`
	Scope        string          `json:"scope,omitempty"`
	Custom       bool            `json:"custom,omitempty"`
}

// Starter is a set of types a project copies (CopyStarter).
type Starter struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Types       []EntryType `json:"types"`
}

// EntryTypeFrom converts a type.
func EntryTypeFrom(t chronicle.EntryType) EntryType {
	return EntryType{ID: t.ID, Title: t.Title, Icon: t.Icon, Description: t.Description, Fields: t.Fields,
		RecordFields: t.RecordFields, ParentTypes: t.ParentTypes, Scope: t.Scope, Custom: t.Custom}
}

// toType converts a type the browser sends.
func (t EntryType) toType() chronicle.EntryType {
	return chronicle.EntryType{ID: t.ID, Title: t.Title, Icon: t.Icon, Description: t.Description,
		RecordFields: t.RecordFields, ParentTypes: t.ParentTypes, Scope: t.Scope}
}

func typesFrom(types []chronicle.EntryType) []EntryType {
	out := make([]EntryType, len(types))
	for i, t := range types {
		out[i] = EntryTypeFrom(t)
	}
	return out
}

// TypesResponseFrom converts the service's TypesResponse; its code is
// carried by the HTTP status rather than the body.
func TypesResponseFrom(resp chronicleservice.TypesResponse) TypesResponse {
	out := TypesResponse{Status: rest.StatusFrom(resp.Common), Types: typesFrom(resp.Types), Starters: []Starter{}}
	for _, s := range resp.Starters {
		out.Starters = append(out.Starters, Starter{ID: s.ID, Title: s.Title, Description: s.Description, Types: typesFrom(s.Types)})
	}
	return out
}
