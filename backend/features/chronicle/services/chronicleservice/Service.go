package chronicleservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
)

// Service keeps the principal's Chronicle entries (Chronicle.md): a tree of
// parents per type, related links, and the rounds that made or changed
// them. Every active entry is kept in the entries table, embedded when an
// embedder is available, so retrieval's chronicle source can find it.
// Entries are private to their owner until they share them: with their
// tenant at once, or with the global tenant once a moderator of their
// tenant and a global moderator approve (MultiTenant.md › Sharing
// Chronicles). Others may read a shared entry but never change it.
type Service interface {
	List(args ListArgs) ListResponse
	Get(args GetArgs) GetResponse
	Create(args CreateArgs) EntryResponse
	Update(args UpdateArgs) EntryResponse
	Delete(args DeleteArgs) DeleteResponse
	Link(args LinkArgs) LinkResponse
	Types(args TypesArgs) TypesResponse
	Reindex(args ReindexArgs) ReindexResponse
	// Share shares one of the principal's entries with Target: "tenant" at
	// once, "global" as a request for moderators, "" to stop sharing (which
	// withdraws a request and removes an approved global copy).
	Share(args ShareArgs) ShareResponse
	// Shares lists the principal's global share requests, or with Queue the
	// requests waiting for them to moderate.
	Shares(args SharesArgs) SharesResponse
	// Moderate approves or rejects a request at its stage: a moderator of
	// the owner's tenant first, then a global moderator, whose approval
	// copies the entry into the global tenant. Nobody moderates their own,
	// and the two approvals come from two people.
	Moderate(args ModerateArgs) ModerateResponse
}

type ShareArgs struct {
	features.ServiceArgs
	ID     uint64
	Target string // chronicle.SharedTenant, chronicle.SharedGlobal, or "" to stop
	Note   string // why, for the moderators
}

type ShareResponse struct {
	Common features.ServiceResponse
	Entry  chronicle.Entry
	Share  *chronicle.ChronicleShare // the global request, when one was made
}

type SharesArgs struct {
	features.ServiceArgs
	Queue bool
}

type SharesResponse struct {
	Common features.ServiceResponse
	Shares []chronicle.ChronicleShare // newest first
}

type ModerateArgs struct {
	features.ServiceArgs
	ShareID uint64
	Approve bool
	Note    string // a reason, on rejecting
}

type ModerateResponse struct {
	Common features.ServiceResponse
	Share  chronicle.ChronicleShare
}

// ListArgs lists the principal's entries, proposed ones included, then the
// active entries shared with them.
type ListArgs struct {
	features.ServiceArgs
}

type ListResponse struct {
	Common  features.ServiceResponse
	Entries []chronicle.Entry // by type, then title
	Links   []chronicle.EntryLink
}

type GetArgs struct {
	features.ServiceArgs
	ID uint64
}

type GetResponse struct {
	Common features.ServiceResponse
	Entry  chronicle.Entry
	Links  []chronicle.EntryLink // to and from the entry
	Rounds []LinkedRound         // newest first
}

// LinkedRound is a round that created or updated the entry.
type LinkedRound struct {
	chronicle.EntryRound
	Text              string // the round's message, cut short
	ConversationTitle string
}

// EntryInput is what the user edits of an entry.
type EntryInput struct {
	Type         string
	Title        string
	ShortSummary string
	FullSummary  string
	FullContext  string
	Fields       json.RawMessage // a JSON object matching the type's Fields schema; empty is {}
	ParentID     uint64          // 0 at the top of its type
}

type CreateArgs struct {
	features.ServiceArgs
	EntryInput
}

// UpdateArgs replaces what the user edits of an entry. Accept makes a
// proposed entry active; Reviewed clears its review flag.
type UpdateArgs struct {
	features.ServiceArgs
	ID uint64
	EntryInput
	Accept   bool
	Reviewed bool
	// RoundID, when set, is the chat round making the change (the
	// chronicle.updateEntry tool): it is linked to the entry with the
	// summaries before, so they can be restored.
	RoundID        uint64
	ConversationID uint64
}

type EntryResponse struct {
	Common features.ServiceResponse
	Entry  chronicle.Entry
}

// DeleteArgs deletes an entry, or dismisses a proposed one. Its children
// move up to its parent, and its links go with it.
type DeleteArgs struct {
	features.ServiceArgs
	ID uint64
}

type DeleteResponse struct {
	Common features.ServiceResponse
}

// LinkArgs links two of the principal's entries as related, or unlinks
// them with Remove. Links have no direction.
type LinkArgs struct {
	features.ServiceArgs
	FromID uint64
	ToID   uint64
	Remove bool
}

type LinkResponse struct {
	Common features.ServiceResponse
	Links  []chronicle.EntryLink // to and from FromID, after the change
}

type TypesArgs struct {
	features.ServiceArgs
}

type TypesResponse struct {
	Common features.ServiceResponse
	Types  []chronicle.EntryType
}

// ReindexArgs brings the entries table up to date: the entries named, or
// with none named every entry whose text changed or was never embedded.
// Background work (serve's start) runs it for every tenant.
type ReindexArgs struct {
	features.ServiceArgs
	EntryIDs []uint64
}

type ReindexResponse struct {
	Common  features.ServiceResponse
	Indexed int // entries written to the table
}
