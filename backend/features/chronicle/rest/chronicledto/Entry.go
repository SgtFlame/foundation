package chronicledto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
)

// Entry is a Chronicle entry. List leaves out its full summary and full
// context, which only the entry editor needs.
type Entry struct {
	ID           uint64          `json:"id,string"`
	Created      time.Time       `json:"created"`
	Updated      time.Time       `json:"updated"`
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	ShortSummary string          `json:"shortSummary"`
	FullSummary  string          `json:"fullSummary,omitempty"`
	FullContext  string          `json:"fullContext,omitempty"`
	Fields       json.RawMessage `json:"fields"`
	ParentID     uint64          `json:"parentId,string,omitempty"`
	Status       string          `json:"status"` // active or proposed
	Review       bool            `json:"review,omitempty"`
	Indexed      bool            `json:"indexed"` // embedded for vector search as it now reads
	TenantID     uint64          `json:"tenantId,string"`
	OwnerID      uint64          `json:"ownerId,string"`
	Shared       string          `json:"shared,omitempty"`          // tenant, or global on the global tenant's copy
	SourceID     uint64          `json:"sourceId,string,omitempty"` // the entry a global copy was made from
}

// EntryFrom converts an entry; full includes its full summary and context.
func EntryFrom(e chronicle.Entry, full bool) Entry {
	out := Entry{
		ID: e.ID, Created: e.CreatedAt, Updated: e.UpdatedAt, Type: e.Type, Title: e.Title, ShortSummary: e.ShortSummary,
		Fields: json.RawMessage(e.Fields), ParentID: e.ParentID, Status: e.Status, Review: e.Review, Indexed: e.IndexedHash != "" && e.IndexedHash == e.Hash(),
		TenantID: e.TenantID, OwnerID: e.OwnerID, Shared: e.Shared, SourceID: e.SourceID,
	}
	if !json.Valid(out.Fields) {
		out.Fields = json.RawMessage(`{}`)
	}
	if full {
		out.FullSummary, out.FullContext = e.FullSummary, e.FullContext
	}
	return out
}

// EntryInput is what the user edits of an entry.
type EntryInput struct {
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	ShortSummary string          `json:"shortSummary"`
	FullSummary  string          `json:"fullSummary"`
	FullContext  string          `json:"fullContext"`
	Fields       json.RawMessage `json:"fields,omitempty"`
	ParentID     uint64          `json:"parentId,string,omitempty"`
}
