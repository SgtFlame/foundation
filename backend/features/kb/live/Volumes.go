// Package kblive contributes the knowledge base's live queries: kb.volumes,
// the Volumes a person may read, which pickers (the agent editor's
// Knowledge card, forge 15a) choose from by id.
package kblive

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Volume is one Volume as kb.volumes lists it.
type Volume struct {
	ID   string `json:"id"`   // its marker's id, as text
	Name string `json:"name"` // its folder's name
	Path string `json:"path"` // from the knowledge base root, with '/'
}

// Every is how often kb.volumes looks again for Volumes made, removed or
// newly readable; a Volume is a folder on disk, which no stream announces.
var Every = 15 * time.Second

// Contribute registers kb.volumes over the knowledge base at root().
func Contribute(reg *extensions.Registry, root func() string) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "kb.volumes", Source: "kb"}, Title: "Volumes",
		Description: "The Volumes (knowledge base folders of markdown files) the person may read, by name: {volumes: [{id, name, path}]}, sorted by name. Replaced whole when a Volume appears, goes or becomes readable. Empty outside the default tenant, which alone has Volumes for now.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Run: func(f live.Feed) error {
			list := func() any { return map[string]any{"volumes": Readable(f.Context(), f.Args().Principal, root())} }
			last := list()
			f.Snapshot(last, live.Position{})
			tick := time.NewTicker(Every)
			defer tick.Stop()
			for {
				select {
				case <-f.Context().Done():
					return nil
				case <-tick.C:
					if v := list(); !reflect.DeepEqual(v, last) {
						last = v
						raw, _ := json.Marshal(v)
						f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
					}
				}
			}
		},
	})
}

// Readable are the Volumes under root that p may read (volume.read), by
// name; none outside the default tenant.
func Readable(ctx context.Context, p features.Principal, root string) []Volume {
	out := []Volume{}
	if root == "" || !kbservice.InTenant(p) {
		return out
	}
	_ = filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if dir != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		marker, err := volumeservice.ReadMarker(dir)
		if err != nil {
			return nil
		}
		res := authz.Tenant(p) + "/" + authz.Resource("volume", marker.ID)
		if authz.Check(ctx, p, "volume.read", res).Allowed {
			out = append(out, Volume{ID: strconv.FormatUint(marker.ID, 10), Name: d.Name(), Path: volumeservice.Rel(root, dir)})
		}
		return filepath.SkipDir // Volumes do not nest
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

