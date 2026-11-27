package chronicle

import (
	"encoding/json"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"gorm.io/gorm"
)

// Memory is what Chronicle's steps read and write (forge 21d-2): the
// chronicle data service's, MemoryIn(gdb) where the tenant database is,
// chronicleservice.Memory(proxy) elsewhere. The steps themselves (and
// their model calls) run where the graph runs.
type Memory interface {
	// Entries is the principal's entries of the given statuses (all when
	// none): its own, or with visible those it may use (shared ones too).
	Entries(args features.ServiceArgs, visible bool, statuses []string) ([]Entry, error)
	// Links is the principal's links touching any of hubs.
	Links(args features.ServiceArgs, hubs []uint64) ([]EntryLink, error)
	// Active is the entries a round pinned: its chronicle segments.
	Active(args features.ServiceArgs, roundID uint64) ([]uint64, error)
	// Apply makes a memory update's changes in one transaction and returns
	// the entries as changed and as made.
	Apply(args features.ServiceArgs, u MemoryUpdate) (updated, proposed []Entry, err error)
	// Flag marks for review the principal's entries a round made or
	// changed.
	Flag(args features.ServiceArgs, round uint64) error
}

// MemoryUpdate is what a round's memory update changes.
type MemoryUpdate struct {
	RoundID, ConversationID uint64
	Updates                 []SummaryUpdate
	// Proposals are new entries; their tenant, owner and status are set
	// by Apply.
	Proposals []Entry
}

// SummaryUpdate rewrites an entry's summaries.
type SummaryUpdate struct {
	ID                        uint64
	ShortSummary, FullSummary string
}

// MemoryIn is the Memory over a tenant database.
func MemoryIn(gdb *gorm.DB) Memory { return dbMemory{gdb} }

type dbMemory struct{ db *gorm.DB }

func (m dbMemory) Entries(args features.ServiceArgs, visible bool, statuses []string) ([]Entry, error) {
	q := Owned(m.db.WithContext(args.Context()), args.Principal)
	if visible {
		q = Visible(m.db.WithContext(args.Context()), args.Principal)
	}
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var list []Entry
	err := q.Find(&list).Error
	return list, err
}

func (m dbMemory) Links(args features.ServiceArgs, hubs []uint64) ([]EntryLink, error) {
	var links []EntryLink
	if len(hubs) == 0 {
		return links, nil
	}
	err := Owned(m.db.WithContext(args.Context()), args.Principal).Where("from_id IN ? OR to_id IN ?", hubs, hubs).Find(&links).Error
	return links, err
}

func (m dbMemory) Active(args features.ServiceArgs, roundID uint64) ([]uint64, error) {
	var segments []retrieval.RoundSegment
	err := m.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).
		Where("round_id = ? AND source_id = ? AND pinned", roundID, "chronicle").Order("number").Find(&segments).Error
	ids := make([]uint64, 0, len(segments))
	for _, seg := range segments {
		if id, err := strconv.ParseUint(seg.SegmentKey, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, err
}

func (m dbMemory) Apply(args features.ServiceArgs, u MemoryUpdate) (updated, proposed []Entry, err error) {
	err = m.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		for _, up := range u.Updates {
			var e Entry
			if err := Owned(tx, args.Principal).Where("status = ?", StatusActive).Limit(1).Find(&e, up.ID).Error; err != nil {
				return err
			}
			if e.ID == 0 {
				continue
			}
			previous, _ := jsonOf(map[string]string{"shortSummary": e.ShortSummary, "fullSummary": e.FullSummary})
			e.ShortSummary, e.FullSummary = up.ShortSummary, up.FullSummary
			if err := tx.Model(&e).Updates(map[string]any{"short_summary": up.ShortSummary, "full_summary": up.FullSummary}).Error; err != nil {
				return err
			}
			if err := tx.Create(&EntryRound{Tenanted: e.Tenanted, EntryID: e.ID, RoundID: u.RoundID, ConversationID: u.ConversationID,
				Action: ActionUpdated, Previous: previous}).Error; err != nil {
				return err
			}
			updated = append(updated, e)
		}
		for _, e := range u.Proposals {
			e.ID = 0
			e.Tenanted, e.Owned, e.Status = db.TenantOf(args.Principal), db.OwnedBy(args.Principal), StatusProposed
			if e.Fields == "" {
				e.Fields = "{}"
			}
			if err := tx.Create(&e).Error; err != nil {
				return err
			}
			if err := tx.Create(&EntryRound{Tenanted: e.Tenanted, EntryID: e.ID, RoundID: u.RoundID, ConversationID: u.ConversationID,
				Action: ActionCreated}).Error; err != nil {
				return err
			}
			proposed = append(proposed, e)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return updated, proposed, nil
}

func (m dbMemory) Flag(args features.ServiceArgs, round uint64) error {
	gdb := m.db.WithContext(args.Context())
	ids := gdb.Model(&EntryRound{}).Select("entry_id").Where("tenant_id = ? AND round_id = ?", args.Principal.TenantID, round)
	return Owned(m.db.WithContext(args.Context()), args.Principal).Model(&Entry{}).Where("id IN (?)", ids).UpdateColumn("review", true).Error
}

func jsonOf(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
