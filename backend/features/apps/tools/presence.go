package apptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
)

// ActorBuilder is the chat's actor in presence (forge 06g question 1).
const ActorBuilder = "builder"

// builderPresence puts a chat round in the presence of the app it changes
// and of each part it touches, as the Builder beside its user, with the
// part it is on (forge 06g). The entries are refreshed while the round
// lasts and cleared when it ends (the chat.roundEnd hook).
type builderPresence struct {
	store  func() *presence.Presence
	mu     sync.Mutex
	rounds map[uint64]*roundPresence
}

// roundPresence is one round's entries: the state last set per object.
type roundPresence struct {
	principal features.Principal
	conn      string
	objects   map[string]json.RawMessage
	stop      chan struct{}
}

func newBuilderPresence(store func() *presence.Presence) *builderPresence {
	return &builderPresence{store: store, rounds: map[uint64]*roundPresence{}}
}

// Where is what one op is working on, in the editors' presence states.
type Where struct {
	Object string // "<type>.<id>", e.g. prefab.contactCard
	State  map[string]any
}

// WhereOf is where an op puts the Builder: always the app, on the
// Applications editor's tab and part ({tab, part}), and the part's own
// editor when it has one: a prefab's key path ({selected}), a graph's
// nodes ({selected: [...]}), the Data Designer's entity type and field.
func WhereOf(appID string, op live.Op) []Where {
	family, name, _ := strings.Cut(op.Command, ".")
	var a map[string]any
	_ = json.Unmarshal(op.Args, &a)
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := a[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	app := map[string]any{}
	out := []Where{}
	switch family {
	case "app":
		item := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(name, "add"), "update"), "remove")
		app["tab"] = map[string]string{
			"Page": "pages", "Nav": "nav", "Kind": "kinds", "EditorKind": "kinds", "Entity": "data", "Command": "commands",
			"Query": "queries", "Role": "roles", "Setting": "settings", "SettingsSchema": "settings", "Trigger": "manifest",
		}[item]
		if app["tab"] == "" {
			app["tab"] = "manifest"
		}
		if part := str("route", "type", "name", "id", "kind", "entity", "key"); part != "" {
			app["part"] = part
		}
	case "entity":
		app["tab"], app["part"] = "data", op.Target
		state := map[string]any{"entity": op.Target}
		if f := str("field", "id"); f != "" && name != "create" {
			state["selected"] = []string{f}
		}
		out = append(out, Where{Object: "records.designer", State: state})
	case "prefab":
		app["tab"], app["part"] = "pages", op.Target
		state := map[string]any{}
		if p := str("path", "parentPath"); p != "" {
			state["selected"] = p
		}
		out = append(out, Where{Object: "prefab." + op.Target, State: state})
	case "bp":
		app["tab"], app["part"] = "blueprints", op.Target
		var nodes []string
		if n, ok := a["node"].(map[string]any); ok {
			if id, ok := n["id"].(string); ok {
				nodes = append(nodes, id)
			}
		}
		if id := str("nodeId", "id"); id != "" {
			nodes = append(nodes, id)
		}
		out = append(out, Where{Object: "graph." + op.Target, State: map[string]any{"selected": nodes}})
	}
	return append([]Where{{Object: "app." + appID, State: app}}, out...)
}

// Touch shows the round's Builder where the batch's ops worked: on each
// object, at the last op's place there.
func (b *builderPresence) Touch(args features.ServiceArgs, appID string, ops []live.Op) {
	store := b.store()
	if store == nil || args.Meta.RoundID == 0 {
		return
	}
	b.mu.Lock()
	r := b.rounds[args.Meta.RoundID]
	if r == nil {
		r = &roundPresence{principal: args.Principal, conn: fmt.Sprintf("builder-%d", args.Meta.RoundID), objects: map[string]json.RawMessage{}, stop: make(chan struct{})}
		b.rounds[args.Meta.RoundID] = r
		go b.refresh(r)
	}
	changed := map[string]json.RawMessage{}
	for _, op := range ops {
		for _, w := range WhereOf(appID, op) {
			if !presence.ValidObject(w.Object) {
				continue
			}
			raw, _ := json.Marshal(w.State)
			r.objects[w.Object], changed[w.Object] = raw, raw
		}
	}
	b.mu.Unlock()
	ctx := context.WithoutCancel(args.Context())
	for object, state := range changed {
		if err := store.SetAs(ctx, r.principal, object, r.conn, ActorBuilder, state); err != nil {
			live.Log.Warn(ctx, "cannot show the Builder in %s: %v", object, err)
		}
	}
}

// refresh keeps the round's entries alive until it ends: a model step may
// take longer than presence's TTL.
func (b *builderPresence) refresh(r *roundPresence) {
	t := time.NewTicker(presence.Refresh)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			b.mu.Lock()
			objects := make(map[string]json.RawMessage, len(r.objects))
			for k, v := range r.objects {
				objects[k] = v
			}
			b.mu.Unlock()
			for object, state := range objects {
				_ = b.store().SetAs(context.Background(), r.principal, object, r.conn, ActorBuilder, state)
			}
		}
	}
}

// End clears the round's entries.
func (b *builderPresence) End(args features.ServiceArgs, roundID uint64) {
	b.mu.Lock()
	r := b.rounds[roundID]
	delete(b.rounds, roundID)
	b.mu.Unlock()
	if r == nil {
		return
	}
	close(r.stop)
	ctx := context.WithoutCancel(args.Context())
	for object := range r.objects {
		b.store().Clear(ctx, r.principal.TenantID, object, r.conn)
	}
}
