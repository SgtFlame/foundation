package chronicledto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
)

// Share is a request to share an entry with the global tenant, with what
// was asked to be shared.
type Share struct {
	ID           uint64     `json:"id,string"`
	Created      time.Time  `json:"created"`
	TenantID     uint64     `json:"tenantId,string"`
	OwnerID      uint64     `json:"ownerId,string"`
	OwnerName    string     `json:"ownerName,omitempty"`
	EntryID      uint64     `json:"entryId,string"`
	Status       string     `json:"status"` // pending, tenantApproved, approved, rejected or withdrawn
	Type         string     `json:"type"`
	Title        string     `json:"title"`
	ShortSummary string     `json:"shortSummary"`
	FullSummary  string     `json:"fullSummary"`
	Note         string     `json:"note,omitempty"`
	TenantBy     uint64     `json:"tenantBy,string,omitempty"`
	TenantAt     *time.Time `json:"tenantAt,omitempty"`
	GlobalBy     uint64     `json:"globalBy,string,omitempty"`
	GlobalAt     *time.Time `json:"globalAt,omitempty"`
	CopyID       uint64     `json:"copyId,string,omitempty"`
}

func ShareFrom(s chronicle.ChronicleShare) Share {
	return Share{
		ID: s.ID, Created: s.CreatedAt, TenantID: s.TenantID, OwnerID: s.OwnerID, OwnerName: s.OwnerName, EntryID: s.EntryID,
		Status: s.Status, Type: s.Type, Title: s.Title, ShortSummary: s.ShortSummary, FullSummary: s.FullSummary, Note: s.Note,
		TenantBy: s.TenantBy, TenantAt: s.TenantAt, GlobalBy: s.GlobalBy, GlobalAt: s.GlobalAt, CopyID: s.CopyID,
	}
}
