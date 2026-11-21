package confighandlers

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config/rest/configdto"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// ChangesQuery is the query the browser follows to refetch changed sections.
const ChangesQuery = "config.changes"

// ContributeLive registers the config.changes query, given the service
// once made: an empty snapshot, then an upsert keyed by section for each
// stored change, until the subscription closes. It replaces the SSE
// stream (01-ServerUI question 9).
func ContributeLive(reg *extensions.Registry, service func() configservice.Service) {
	live.Queries.Register(reg, live.Query{
		Descriptor:  extensions.Descriptor{ID: ChangesQuery, Source: "config"},
		Title:       "Settings changes",
		Description: "Each stored change of a settings section, as {section, scope}, keyed by section, from when the subscription opens; read the section again to see its value.",
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Run: func(f live.Feed) error {
			watch := service().Watch(configservice.WatchArgs{ServiceArgs: f.Args()})
			f.Snapshot(map[string]any{}, live.Position{})
			for e := range watch.Events {
				raw, _ := json.Marshal(configdto.ChangedEvent{Section: e.Section, Scope: e.Scope})
				f.Change(live.Delta{Op: "upsert", Key: e.Section, Value: raw}, live.Position{})
			}
			return nil
		},
	})
}
