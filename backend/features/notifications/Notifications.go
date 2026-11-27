// Package notifications is one place every app sends what a person must
// see (forge 26e-1, documentation/Notifications.md): a mention, a review
// waiting, mail, an agent's question. A notification is a record of the
// tenant's notification entity type, which only its recipient reads,
// marks read or dismisses; apps send them with Notify (the notify.send
// node), deduplicated by a key so a retried job notifies once.
package notifications

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

var log = logging.In("notifications")

// Entity is the notifications' entity type.
const Entity = "notification"

// Action is what sending a notification needs: notification.create, held
// by apps in their tenant with no grant.
const Action = "notification.create"

//go:embed notification.json
var entityJSON []byte

// EntityJSON is the entity type as defined, which Ensure imports.
func EntityJSON() json.RawMessage { return entityJSON }

// Deps are the records' (notifications are records).
type Deps = data.Deps

// Notification is one to send.
type Notification struct {
	// Recipients are user ids; one record is made per recipient.
	Recipients []string `json:"recipients"`
	App        string   `json:"app,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Link       string   `json:"link,omitempty"`
	// Key makes a notification unique for its recipient
	// (<app>:<subject>:<kind>): sent again with the same key, nothing new
	// is made. Empty never deduplicates.
	Key string `json:"key,omitempty"`
}

// ensured are the tenants whose entity type is published, by the
// projections they were read from (so a test's fresh server is asked
// again), a cache of the projection.
var ensured sync.Map

type ensuredKey struct {
	store  any
	tenant uint64
}

// Ensure makes sure the tenant's notification entity type is published,
// importing and publishing it as the tenant's service principal the first
// time. Every process may do this at once: the commands' ids make the
// streams drop the copies.
func Ensure(ctx context.Context, d Deps, tenant uint64) error {
	key := ensuredKey{d.Projections, tenant}
	if _, ok := ensured.Load(key); ok {
		return nil
	}
	if v, ok, err := d.Projections.Get(ctx, tenant, entities.PublishedProjector, "entity."+Entity); err == nil && ok && v.Value != nil {
		ensured.Store(key, true)
		return nil
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(tenant)}
	var e map[string]any
	_ = json.Unmarshal(entityJSON, &e)
	raw, _ := json.Marshal(map[string]any{"entity": e})
	res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "notifications-entity-import-1", Type: "entity.import", Target: Entity, Args: raw})
	if res.Common.Code >= 300 {
		return log.Code("notifications_unavailable").Errorf("cannot make the notification entity type: %s", res.Common.Message)
	}
	v, err := d.Projections.WaitFor(ctx, tenant, entities.DraftProjector, "entity."+Entity, res.Seq, 5*time.Second)
	if err != nil {
		return log.Wrap(err)
	}
	var draft entities.Draft
	_ = json.Unmarshal(v.Value, &draft)
	if draft.Published == 0 || draft.PublishedRev != draft.Rev {
		raw, _ = json.Marshal(map[string]any{"rev": draft.Rev})
		res = d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "notifications-entity-publish-1", Type: "entity.publish", Target: Entity, Args: raw})
		if res.Common.Code >= 300 {
			return log.Code("notifications_unavailable").Errorf("cannot publish the notification entity type: %s", res.Common.Message)
		}
		if _, err := d.Projections.WaitFor(ctx, tenant, entities.PublishedProjector, "entity."+Entity, res.Seq, 5*time.Second); err != nil {
			return log.Wrap(err)
		}
	}
	ensured.Store(key, true)
	return nil
}

// RecordID is a notification's record id: from its recipient and key, so
// the same key makes one record, else new each time.
func RecordID(recipient, key string) string {
	if key == "" {
		return strconv.FormatUint(features.NewTSID(), 10)
	}
	sum := sha256.Sum256([]byte(recipient + "\x00" + key))
	return "n" + hex.EncodeToString(sum[:])[:24]
}

// Notify sends n to each recipient as args' principal, which needs
// notification.create in its tenant. The records are made by the tenant's
// service principal, since no one but the recipient may touch them.
// Returns the record ids, one per recipient, made now or before.
func Notify(args features.ServiceArgs, d Deps, n Notification) ([]string, *features.ServiceResponse) {
	ctx := args.Context()
	p := args.Principal
	if err := authz.Check(ctx, p, Action, authz.Tenant(p)).Err(Action, authz.Tenant(p)); err != nil {
		return nil, args.Fail(authz.Status, err)
	}
	if strings.TrimSpace(n.Title) == "" {
		return nil, args.Fail(http.StatusBadRequest, log.Code("bad_notification").New("a notification needs a title"))
	}
	if d.Bus == nil || d.Projections == nil {
		return nil, args.Fail(http.StatusServiceUnavailable, log.Code("notifications_unavailable").New("there are no notifications"))
	}
	if err := Ensure(ctx, d, p.TenantID); err != nil {
		return nil, args.Fail(http.StatusServiceUnavailable, err)
	}
	if n.App == "" && p.Kind == features.PrincipalApp {
		n.App = p.App
	}
	as := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(p.TenantID), Meta: args.Meta}
	ids := []string{}
	seen := map[string]bool{}
	for _, to := range n.Recipients {
		to = strings.TrimSpace(to)
		if to == "" || to == "0" || seen[to] {
			continue
		}
		seen[to] = true
		id := RecordID(to, n.Key)
		key := data.Key(Entity, id)
		if v, ok, _ := d.Projections.Get(ctx, p.TenantID, data.Projector, key); ok && v.Value != nil {
			ids = append(ids, id) // sent before
			continue
		}
		raw, _ := json.Marshal(map[string]any{"entity": Entity, "values": map[string]any{
			"recipient": to, "app": n.App, "kind": n.Kind, "title": n.Title, "body": n.Body, "link": n.Link, "key": n.Key, "read": false}})
		res := d.Bus.Execute(bus.Request{ServiceArgs: as, ID: "notify-" + id, Type: "record.create", Target: id, Args: raw})
		if res.Common.Code >= 300 {
			// Made meanwhile by another process: the same notification
			if v, ok, _ := d.Projections.Get(ctx, p.TenantID, data.Projector, key); ok && v.Value != nil {
				ids = append(ids, id)
				continue
			}
			return ids, &res.Common
		}
		ids = append(ids, id)
	}
	return ids, nil
}
