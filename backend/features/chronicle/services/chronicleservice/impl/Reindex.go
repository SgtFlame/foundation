package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// Reindex brings the entries table up to date, synchronously: the named
// entries, or every entry whose text the table does not hold embedded,
// removing rows whose entry is gone or no longer active. A service
// principal reindexes every tenant; a user only their own entries.
func (s *service) Reindex(args chronicleservice.ReindexArgs) chronicleservice.ReindexResponse {
	if failed := check(args.ServiceArgs, "chronicle.write", 0); failed != nil {
		return chronicleservice.ReindexResponse{Common: *failed}
	}
	resp := chronicleservice.ReindexResponse{Common: features.OK()}
	if s.store == nil {
		return resp
	}
	fail := func(err error) chronicleservice.ReindexResponse {
		return chronicleservice.ReindexResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	q := s.db.WithContext(args.Context())
	if args.Principal.Kind != features.PrincipalService {
		q = chronicle.Owned(q, args.Principal)
	}
	if len(args.EntryIDs) > 0 {
		q = q.Where("id IN ?", args.EntryIDs)
	}
	var entries []chronicle.Entry
	if err := q.Find(&entries).Error; err != nil {
		return fail(err)
	}

	byID := make(map[uint64]chronicle.Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	var stale []uint64
	if len(args.EntryIDs) > 0 {
		for _, e := range entries {
			stale = append(stale, e.ID)
		}
	} else {
		stored, err := s.store.Hashes(args.Context())
		if err != nil {
			return fail(err)
		}
		for _, e := range entries {
			if e.Status == chronicle.StatusActive && stored[e.ID] != e.Hash() {
				stale = append(stale, e.ID)
			}
		}
		// Only a service principal sees every row, so only it removes the
		// rows of deleted entries
		if args.Principal.Kind == features.PrincipalService {
			for id := range stored {
				if _, ok := byID[id]; !ok {
					if err := s.store.Delete(args.Context(), id); err != nil {
						return fail(err)
					}
				}
			}
		}
		// The table no longer holds what IndexedHash says, so it is reset
		if len(stale) > 0 {
			if err := s.db.WithContext(args.Context()).Model(&chronicle.Entry{}).Where("id IN ?", stale).UpdateColumn("indexed_hash", "").Error; err != nil {
				return fail(err)
			}
		}
	}
	for _, id := range stale {
		call := args.ServiceArgs
		if call.Principal.Kind == features.PrincipalService {
			// Usage is attributed to the entry's owner
			e := byID[id]
			call.Principal = features.Principal{Kind: features.PrincipalUser, UserID: e.OwnerID, TenantID: e.TenantID}
		}
		wrote, err := s.indexNow(call, id)
		if err != nil {
			return fail(err)
		}
		if wrote {
			resp.Indexed++
		}
	}
	return resp
}
