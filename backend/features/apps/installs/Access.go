package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// AccessFollower makes and removes an app's roles as it is installed,
// upgraded and uninstalled (forge 05c).
const AccessFollower = "apps.access"

// followAccess makes sure the principal's tenant keeps its apps' roles in
// step with their installs: each install and upgrade makes the version's
// roles what it says (SyncAppRoles, as the system: forge 05c question 1),
// an install gives the installer the installer's role on the tenant, and
// an uninstall removes the roles with their grants.
func followAccess(args features.ServiceArgs, deps Deps) error {
	if deps.Roles == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	t := live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, AccessFollower, []string{t.Subjects(tenant)}, func(e live.Event) error {
		ctx := context.Background()
		app := e.Target.ID
		if e.Type == EventUninstalled {
			return deps.Roles.RemoveAppRoles(ctx, e.TenantID, app)
		}
		var c change
		if err := json.Unmarshal(e.Data, &c); err != nil || c.Version == nil {
			return fmt.Errorf("cannot read the install: %v", err)
		}
		ids, err := deps.Roles.SyncAppRoles(ctx, e.TenantID, app, apps.AppRoles(*c.Version))
		if err != nil {
			return err
		}
		role := ids[apps.InstallerRole(c.Version.Manifest)]
		if e.Type != EventInstalled || role == 0 || e.UserID == 0 {
			return nil
		}
		_, err = deps.Roles.Grant(ctx, nil, authz.GrantArgs{UserID: e.UserID, RoleID: role, Resource: authz.Resource("tenant", e.TenantID)})
		return err
	})
}

// cache keeps each tenant's running installs in memory, from a watch on
// the apps.install projection, for what is asked too often to read the
// projection each time: where an entity type's records are (Placement)
// and the apps' settings sections.
type cache struct {
	store   live.Store
	mu      sync.Mutex
	tenants map[uint64]*running
}

type running struct {
	ready chan struct{}
	mu    sync.RWMutex
	apps  map[string]apps.Install
}

var caches sync.Map // live.Store → *cache

func cacheOf(store live.Store) *cache {
	if store == nil {
		return &cache{}
	}
	c, _ := caches.LoadOrStore(store, &cache{store: store, tenants: map[uint64]*running{}})
	return c.(*cache)
}

// installed is the tenant's running installs by app, as the watch last
// saw them; the first call for a tenant starts its watch and waits for it
// (at most two seconds). None while the store is not made yet (startup).
func (c *cache) installed(tenant uint64) map[string]apps.Install {
	if c.store == nil {
		return nil
	}
	c.mu.Lock()
	r := c.tenants[tenant]
	if r == nil {
		r = &running{ready: make(chan struct{}), apps: map[string]apps.Install{}}
		c.tenants[tenant] = r
		go r.watch(c, tenant)
	}
	c.mu.Unlock()
	select {
	case <-r.ready:
	case <-time.After(2 * time.Second):
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.apps
}

// watch follows the tenant's installs for as long as the server runs.
func (r *running) watch(c *cache, tenant uint64) {
	entries, err := c.store.Watch(context.Background(), tenant, Projector, TargetType+".*")
	if err != nil {
		log.Error(context.Background(), err, "apps: cannot watch the installs of tenant %d", tenant)
		c.mu.Lock()
		delete(c.tenants, tenant) // the next call tries again
		c.mu.Unlock()
		close(r.ready)
		return
	}
	once := sync.Once{}
	for e := range entries {
		if e == nil {
			once.Do(func() { close(r.ready) })
			continue
		}
		var in apps.Install
		ok := !e.Deleted && json.Unmarshal(e.Value.Value, &in) == nil && in.Installed()
		r.mu.Lock()
		next := make(map[string]apps.Install, len(r.apps)+1) // readers keep the map they were given
		for k, v := range r.apps {
			next[k] = v
		}
		if ok {
			next[in.App] = in
		} else {
			delete(next, e.Key[len(TargetType)+1:])
		}
		r.apps = next
		r.mu.Unlock()
	}
	once.Do(func() { close(r.ready) })
}

// Placement is where records.Resource finds an entity type's app: an
// installed app's own types are under its path (forge 05c question 3);
// the tenant's, and Go apps' (Hello), stay at the tenant's.
func Placement(deps func() Deps) records.Placement {
	return func(tenant uint64, entity string) string {
		for app, in := range cacheOf(deps().Projections).installed(tenant) {
			if slices.Contains(in.Version.Owned, entity) {
				return app
			}
		}
		return ""
	}
}
