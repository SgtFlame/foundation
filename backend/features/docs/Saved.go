package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
)

// FollowSaves marks a page saved (page.saved, 35a-2) whenever its document
// file is written, by a room's writer or anyone else: it sets the page
// record's lastSavedAt and hash as the Volumes' tenant's service
// principal; a file of no page is ignored. Subscribers
// of file events must not block, so each update is sent from its own
// goroutine.
func FollowSaves(ctx context.Context, root string, events volumeservice.FileEvents, b func() *bus.Bus) (stop func()) {
	if events == nil {
		return func() {}
	}
	return events.Subscribe(func(e volumeservice.FileEvent) {
		if e.Op != volumeservice.FileChanged {
			return
		}
		page, hash, ok := SavedPage(root, e.File)
		if !ok {
			return
		}
		go MarkSaved(ctx, b(), page, hash, time.Now().UTC())
	})
}

// MarkSaved sends page.saved for one page: record.update of lastSavedAt and
// hash, refused quietly when the page has no record (a file made by hand).
func MarkSaved(ctx context.Context, b *bus.Bus, page, hash string, at time.Time) {
	if b == nil {
		return
	}
	raw, _ := json.Marshal(map[string]any{"entity": EntityPage, "set": map[string]any{"lastSavedAt": at.Format(time.RFC3339), "hash": hash}})
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(features.LocalTenantID, "docs")}
	res := b.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("docs-saved-%s-%s-%d", page, hash[:16], at.UnixMilli()), Type: "record.update", Target: page, Args: raw})
	if res.Common.Code >= 300 {
		log.Debug(ctx, "page %s was saved but not marked: %s", page, res.Common.Message)
	}
}
