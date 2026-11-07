package impl

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	"gorm.io/gorm"
)

var log = apps.Log.Tags("transfer")

// Deps are what the service works on.
type Deps struct {
	Projections   manifests.Projections
	DB            *gorm.DB // the tenant database, holding the published versions
	Bus           *bus.Bus
	Subscriptions *subscribe.Subscriptions
	Registry      *extensions.Registry
}

type service struct {
	deps func() Deps
}

// check is authz.Check as a failure, nil when allowed.
func check(args features.ServiceArgs, action, resource string) *features.ServiceResponse {
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// env is this server's primitives, which documents are checked against.
func env(reg *extensions.Registry) apps.DocumentEnv {
	return apps.DocumentEnv{
		Catalog: ui.Catalog(reg),
		Node: func(typ string) bool {
			return slices.ContainsFunc(cognition.Nodes.All(reg), func(n cognition.NodeType) bool { return n.ID == typ })
		},
		Builtin: func(id string) (cognition.Graph, bool) {
			all := cognition.Graphs.All(reg)
			if i := slices.IndexFunc(all, func(b cognition.BuiltIn) bool { return b.ID == id }); i >= 0 {
				return all[i].Graph, true
			}
			return cognition.Graph{}, false
		},
		Query: func(name string) bool {
			return slices.ContainsFunc(live.Queries.All(reg), func(q live.Query) bool { return q.ID == name })
		},
	}
}

// kinds are where each part type lives.
var kinds = map[string]struct{ target, draft, published string }{
	apps.PartPrefab: {prefabs.TargetType, prefabs.DraftProjector, prefabs.PublishedProjector},
	apps.PartGraph:  {drafts.TargetType, drafts.DraftProjector, drafts.PublishedProjector},
	apps.PartEntity: {entities.TargetType, entities.DraftProjector, entities.PublishedProjector},
}

// get reads a projected value of the principal's tenant.
func get(ctx context.Context, store live.Store, tenant uint64, projector, key string) (json.RawMessage, bool) {
	v, ok, err := store.Get(ctx, tenant, projector, key)
	if err != nil || !ok || v.Value == nil {
		return nil, false
	}
	return v.Value, true
}

// draftOf is what every part's draft has in common.
type draftOf struct {
	Rev          int             `json:"rev"`
	Published    int             `json:"published"`
	PublishedRev int             `json:"publishedRev"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Prefab       json.RawMessage `json:"prefab"`
	Graph        json.RawMessage `json:"graph"`
	Entity       json.RawMessage `json:"entity"`
}

func (d draftOf) definition() json.RawMessage {
	switch {
	case d.Prefab != nil:
		return d.Prefab
	case d.Graph != nil:
		return d.Graph
	}
	return d.Entity
}

// part reads a part's draft in the principal's tenant.
func part(args features.ServiceArgs, d Deps, typ, id string) (draftOf, bool) {
	var dr draftOf
	k := kinds[typ]
	raw, ok := get(args.Context(), d.Projections, args.Principal.TenantID, k.draft, k.target+"."+id)
	return dr, ok && json.Unmarshal(raw, &dr) == nil
}

// published reads a part's last published definition in the principal's
// tenant.
func published(args features.ServiceArgs, d Deps, typ, id string) (json.RawMessage, bool) {
	k := kinds[typ]
	return get(args.Context(), d.Projections, args.Principal.TenantID, k.published, k.target+"."+id)
}
