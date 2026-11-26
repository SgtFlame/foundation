// Package chroniclesource contributes Chronicle entries to retrieval as the
// chronicle source (documentation/ContextRetrieval.md): the
// principal's active entries, on the Chronicle side of the
// Chronicle ↔ Library dial. An agent's run searches the memory of the
// person it may (Principal.MemoryOf, forge 15a): their own entries and the
// tenant's shared ones, never the global tenant's.
package chroniclesource

import (
	"context"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Contribute registers the chronicle source, searching store. A segment's
// ID and feedback key are its entry's ID, so 👍 and 👎 follow the entry
// through edits.
func Contribute(reg *extensions.Registry, store chronicle.Store) {
	retrieval.Sources.Register(reg, retrieval.Source{
		Descriptor: extensions.Descriptor{ID: chroniclesteps.SourceID, Order: 100, Source: "chronicle"},
		Title:      "Chronicles",
		Axis:       retrieval.AxisChronicle,
		VectorSearch: func(ctx context.Context, vector []float32, k int, _ retrieval.Filter) ([]retrieval.Segment, error) {
			owner, global, ok := ownerOf(ctx)
			if !ok {
				return nil, nil
			}
			return search(owner, global, k, func(o chronicle.Owner) ([]chronicle.Match, error) { return store.VectorSearch(ctx, o, vector, k) })
		},
		KeywordSearch: func(ctx context.Context, query string, k int, _ retrieval.Filter) ([]retrieval.Segment, error) {
			owner, global, ok := ownerOf(ctx)
			if !ok {
				return nil, nil
			}
			return search(owner, global, k, func(o chronicle.Owner) ([]chronicle.Match, error) { return store.KeywordSearch(ctx, o, query, k) })
		},
	})
}

// search runs one search over the owner's tenant (their entries and those
// shared with the tenant) and over the global tenant's shared entries, and
// interleaves the two rankings, best first.
func search(owner chronicle.Owner, global bool, k int, run func(chronicle.Owner) ([]chronicle.Match, error)) ([]retrieval.Segment, error) {
	mine, err := run(owner)
	if err != nil || !global || owner.TenantID == features.GlobalTenantID {
		return segments(mine), err
	}
	shared, err := run(chronicle.Owner{TenantID: features.GlobalTenantID, UserID: owner.UserID, OthersOnly: true})
	if err != nil {
		return segments(mine), nil // the global tenant's table is optional
	}
	out := make([]chronicle.Match, 0, min(k, len(mine)+len(shared)))
	for i := 0; len(out) < k && (i < len(mine) || i < len(shared)); i++ {
		if i < len(mine) {
			out = append(out, mine[i])
		}
		if i < len(shared) && len(out) < k {
			out = append(out, shared[i])
		}
	}
	return segments(out), nil
}

// ownerOf is the user whose entries a call searches, and whether the
// global tenant's shared entries are searched too: a user's own, with the
// global ones; an agent's run's MemoryOf, without (forge 15a). A call by
// anyone else searches none.
func ownerOf(ctx context.Context) (chronicle.Owner, bool, bool) {
	p, _, ok := features.CallFrom(ctx)
	switch {
	case !ok:
		return chronicle.Owner{}, false, false
	case p.Kind == features.PrincipalUser:
		return chronicle.Owner{TenantID: p.TenantID, UserID: p.UserID}, true, true
	case p.Kind == features.PrincipalAgent && p.MemoryOf != 0:
		return chronicle.Owner{TenantID: p.TenantID, UserID: p.MemoryOf}, false, true
	}
	return chronicle.Owner{}, false, false
}

func segments(matches []chronicle.Match) []retrieval.Segment {
	out := make([]retrieval.Segment, len(matches))
	for i, m := range matches {
		id := strconv.FormatUint(m.EntryID, 10)
		out[i] = retrieval.Segment{
			SourceID: chroniclesteps.SourceID, ID: id, Key: id, Title: m.Title, Text: m.Text,
			Location: retrieval.Location{EntryID: m.EntryID},
		}
	}
	return out
}
