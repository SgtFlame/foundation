package impl

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"gorm.io/gorm"
)

// index brings one entry's row in the entries table up to date in the
// background, after the request has answered.
func (s *service) index(args features.ServiceArgs, id uint64) {
	if s.store == nil {
		return
	}
	args.Ctx = context.WithoutCancel(args.Context())
	s.indexing.Add(1)
	go func() {
		defer s.indexing.Done()
		if _, err := s.indexNow(args, id); err != nil {
			log.With("entryId", id).Error(args.Context(), err, "cannot index entry %d", id)
		}
	}()
}

// indexNow writes the entry as it now is: an active entry is embedded
// (when that fails it is still found by keywords, and Reindex tries
// again), anything else is removed. It reports whether it wrote a row.
func (s *service) indexNow(args features.ServiceArgs, id uint64) (bool, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	ctx := args.Context()
	var e chronicle.Entry
	err := s.db.WithContext(ctx).First(&e, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && e.Status != chronicle.StatusActive) {
		return false, s.store.Delete(ctx, id)
	}
	if err != nil {
		return false, err
	}
	hash := e.Hash()
	if hash == e.IndexedHash {
		return false, nil
	}
	vector := s.embed(args, e)
	if err := s.store.Put(ctx, chronicle.Row{EntryID: e.ID, TenantID: e.TenantID, OwnerID: e.OwnerID, Shared: e.Shared != "", Hash: hash, Title: e.Title, Text: e.Text(), Vector: vector}); err != nil {
		return false, err
	}
	if vector == nil {
		return true, nil // found by keywords until it can be embedded
	}
	return true, s.db.WithContext(ctx).Model(&e).UpdateColumn("indexed_hash", hash).Error
}

// embed returns the entry's vector, or nil when it cannot be embedded (no
// embedder, no API key); the failure is logged by Fail.
func (s *service) embed(args features.ServiceArgs, e chronicle.Entry) []float32 {
	if s.embedder == nil {
		return nil
	}
	call := args
	call.Meta = call.Meta.WithFeature("embed-chronicle")
	resp := s.embedder.Embed(embedservice.EmbedArgs{ServiceArgs: call, Texts: []string{e.Text()}, Purpose: embedservice.PurposeDocument, Title: e.Title})
	if resp.Common.Code >= 300 || len(resp.Vectors) == 0 || len(resp.Vectors[0]) != s.store.Dimensions() {
		return nil
	}
	return resp.Vectors[0]
}

// clip cuts s to at most n bytes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
