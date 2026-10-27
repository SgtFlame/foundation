// Package chronicle is Chronicle memory (documentation/Chronicle.md):
// entries, kept per user as a tree of parents with related links between
// them, and what the conversation rounds did to them. chronicleservice
// edits the entries; the chronicle context provider runs stage 1 (context
// matching) for each round and the chronicle round follower the memory
// update after it, both through the step functions in chronicle/steps.
package chronicle

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"gorm.io/gorm"
)

// Entry statuses.
const (
	StatusActive   = "active"
	StatusProposed = "proposed" // made by the memory update; not used until the user accepts it
)

// Entry is one memory entry, a Chronicle in the UI. Stage 1 reads its short
// summary; the reply gets its full summary, or its full context when that
// is short.
type Entry struct {
	db.Model
	db.Tenanted
	db.Owned
	Type         string `gorm:"index"` // a built-in EntryType ID
	Title        string
	ShortSummary string // one or two sentences
	FullSummary  string // the core facts, markdown
	FullContext  string // the raw, detailed payload
	Fields       string // JSON object: the type's fields, e.g. an event's date
	ParentID     uint64 `gorm:"index"` // 0 at the top of its type; checking an entry includes its parents
	Status       string `gorm:"index"` // StatusActive or StatusProposed
	Review       bool   // flagged for review: a round that made or changed it was 🚫 ignored
	IndexedHash  string // the Hash in the entries table; differs while the entry waits to be indexed
	// Shared is SharedTenant when the owner shares it with their tenant, and
	// SharedGlobal on the global tenant's copy of an approved global share.
	Shared string `gorm:"index"`
	// SourceID is the entry a global copy was made from.
	SourceID uint64 `gorm:"index"`
}

// Sharing (MultiTenant.md › Sharing Chronicles).
const (
	SharedTenant = "tenant" // other members of the owner's tenant use it at once
	SharedGlobal = "global" // a copy in the global tenant, once two moderators approve
)

// Text is what the entries table indexes and the reply reads: the title,
// the short summary and the full summary, or the full context when there
// is no full summary.
func (e Entry) Text() string {
	body := e.FullSummary
	if strings.TrimSpace(body) == "" {
		body = e.FullContext
	}
	parts := []string{"# " + strings.TrimSpace(e.Title)}
	for _, p := range []string{e.ShortSummary, body} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Hash identifies Text and whether the entry is shared, so an unchanged
// entry is not written to the entries table again.
func (e Entry) Hash() string {
	text := e.Text()
	if e.Shared != "" {
		text += "\x00shared"
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

// Link kinds. A parent is Entry.ParentID, not a link.
const LinkRelated = "related"

// EntryLink is an edge of the memory graph besides the parent tree:
// stage 1 sees an active entry's linked entries as candidates.
type EntryLink struct {
	db.Model
	db.Tenanted
	db.Owned
	FromID uint64 `gorm:"index"`
	ToID   uint64 `gorm:"index"`
	Kind   string
}

// Round actions.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
)

// EntryRound records a round that created or updated an entry: the entry's
// "linked conversation rounds".
type EntryRound struct {
	db.Model
	db.Tenanted
	EntryID        uint64 `gorm:"index"`
	RoundID        uint64 `gorm:"index"`
	ConversationID uint64
	Action         string
	// Previous is the entry's short and full summaries before an update,
	// as JSON, so a change the user rejects can be undone.
	Previous string
}

// Share statuses: a global share needs a moderator of the owner's tenant,
// then a global moderator.
const (
	SharePending        = "pending"
	ShareTenantApproved = "tenantApproved"
	ShareApproved       = "approved"
	ShareRejected       = "rejected"
	ShareWithdrawn      = "withdrawn"
)

// ChronicleShare is a request to share an entry with the global tenant. It
// keeps what the owner asked to share, so moderators approve what they
// read; the approved copy is made from it.
type ChronicleShare struct {
	db.Model
	db.Tenanted // the owner's tenant
	db.Owned
	EntryID      uint64 `gorm:"index"`
	Status       string `gorm:"index"`
	OwnerName    string
	Type         string
	Title        string
	ShortSummary string
	FullSummary  string
	Note         string // the owner's reason, or a moderator's on rejecting
	TenantBy     uint64 // the tenant moderator who approved or rejected
	TenantAt     *time.Time
	GlobalBy     uint64 // the global moderator
	GlobalAt     *time.Time
	CopyID       uint64 // the global tenant's copy, once approved
}

// Models are the chronicle feature's tables, for db.Migrate.
func Models() []any {
	return []any{&Entry{}, &EntryLink{}, &EntryRound{}, &ChronicleShare{}}
}

// Owned is the principal's own entries, links or rounds of entries.
func Owned(gdb *gorm.DB, p features.Principal) *gorm.DB {
	return gdb.Scopes(db.InTenant(p.TenantID)).Where("owner_id = ?", p.UserID)
}

// Visible is the entries the principal may use: their own, those shared
// with their tenant, and the global tenant's but the copies of their own.
func Visible(gdb *gorm.DB, p features.Principal) *gorm.DB {
	return gdb.Where("(tenant_id = ? AND (owner_id = ? OR shared = ?)) OR (tenant_id = ? AND shared = ? AND owner_id <> ?)",
		p.TenantID, p.UserID, SharedTenant, features.GlobalTenantID, SharedGlobal, p.UserID)
}

// WithAncestors is ids followed by every ancestor of theirs not already
// among them, nearest first; parents are looked up in byID. A cycle stops
// the walk.
func WithAncestors(ids []uint64, byID map[uint64]Entry) []uint64 {
	seen := make(map[uint64]bool, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range ids {
		for parent := byID[id].ParentID; parent != 0 && !seen[parent]; parent = byID[parent].ParentID {
			if _, ok := byID[parent]; !ok {
				break
			}
			seen[parent] = true
			out = append(out, parent)
		}
	}
	return out
}
