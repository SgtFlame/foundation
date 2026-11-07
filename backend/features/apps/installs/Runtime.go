package installs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Resolver is how the bus or the subscriptions find a command or query no
// feature registered.
type Resolver[T any] func(args features.ServiceArgs, typ string) (T, bool)

// CommandFor finds an installed app's command (<appId>.<type>), decided by
// the command graph the installed version pinned; else what inner (the
// tenant's published command graphs) finds, but for a graph owned by an
// app made from a manifest, which is live only through its app's install
// (forge 05b question 3).
func CommandFor(deps func() Deps, inner Resolver[live.Command]) Resolver[live.Command] {
	return func(args features.ServiceArgs, typ string) (live.Command, bool) {
		d := deps()
		if app, name, ok := apps.DraftUnscoped(typ); ok {
			return draftCommand(args, d, typ, app, name)
		}
		if app, name, ok := apps.Unscoped(typ); ok {
			if in, ok := Running(args.Context(), d.Projections, args.Principal.TenantID, app); ok {
				if c, g, ok := in.Version.Command(name); ok {
					return command(d, typ, app, name, c, g), true
				}
			}
		}
		if inner == nil {
			return live.Command{}, false
		}
		cmd, ok := inner(args, typ)
		if !ok || manifestOwned(args, d, cmd.Source) {
			return live.Command{}, false
		}
		return cmd, true
	}
}

// QueryFor is CommandFor for queries: an installed app's queries are
// <appId>.<name>, answered by the query graph its version pinned.
func QueryFor(deps func() Deps, inner Resolver[live.Query]) Resolver[live.Query] {
	return func(args features.ServiceArgs, typ string) (live.Query, bool) {
		d := deps()
		if app, name, ok := apps.DraftUnscoped(typ); ok {
			return draftQuery(args, d, typ, app, name)
		}
		if app, name, ok := apps.Unscoped(typ); ok {
			if in, ok := Running(args.Context(), d.Projections, args.Principal.TenantID, app); ok {
				if _, g, ok := in.Version.Query(name); ok {
					return query(d, typ, app, name, g), true
				}
			}
		}
		if inner == nil {
			return live.Query{}, false
		}
		q, ok := inner(args, typ)
		if !ok || manifestOwned(args, d, q.Source) {
			return live.Query{}, false
		}
		return q, true
	}
}

// manifestOwned tells whether a resolved graph (source graph:<id>) belongs
// to an app made from a manifest. Apps written in Go (Hello) own graphs
// that are live by themselves.
func manifestOwned(args features.ServiceArgs, d Deps, source string) bool {
	id, ok := strings.CutPrefix(source, "graph:")
	if !ok || d.Nodes.Engine == nil {
		return false
	}
	g, ok := d.Nodes.Engine.Graph(args, id)
	if !ok {
		return false
	}
	app, ok := strings.CutPrefix(g.Owner, "app:")
	if !ok {
		return false
	}
	_, ok, _ = d.Projections.Get(args.Context(), args.Principal.TenantID, manifests.DraftProjector, manifests.TargetType+"."+app)
	return ok
}

// command is an installed app's command. Its decision reads the install
// again, so one decided after an upgrade runs the new version's graph, and
// one the new version removed is refused.
func command(d Deps, typ, app, name string, c apps.Command, g cognition.Graph) live.Command {
	spec, _ := specOf[livenodes.CommandSpec](g.Spec)
	action, resource := actionOf(c, spec)
	return live.Command{
		Descriptor: extensions.Descriptor{ID: typ, Source: "app:" + app}, Title: spec.Title, Description: spec.Description,
		Args: spec.Args, Events: spec.Events, Area: live.Area(spec.Area), TargetType: spec.TargetType, Projection: spec.Projection,
		Action: action, Resource: resource,
		Decide: func(dec live.Decision) ([]live.NewEvent, []live.Problem) {
			in, ok := Running(dec.Context(), d.Projections, dec.Principal.TenantID, app)
			if !ok {
				return nil, []live.Problem{{Code: "app_not_installed", Message: fmt.Sprintf("the app %s is not installed", app)}}
			}
			_, g, ok := in.Version.Command(name)
			if !ok {
				return nil, []live.Problem{{Code: "app_command_removed", Message: fmt.Sprintf("the app %s has no command %s since version %d", app, name, in.Version.Version)}}
			}
			return livenodes.Decide(d.Nodes, dec, typ, g.ID, &g)
		},
	}
}

// query is an installed app's query. It runs again when the install
// changes, under the version installed then: a fresh snapshot when the
// query's graph changed, and the subscription ends when the app is
// uninstalled or the query removed.
func query(d Deps, typ, app, name string, g cognition.Graph) live.Query {
	spec, _ := specOf[livenodes.QuerySpec](g.Spec)
	return live.Query{
		Descriptor: extensions.Descriptor{ID: typ, Source: "app:" + app}, Title: spec.Title, Description: spec.Description,
		Params: spec.Params, Action: spec.Action,
		Run: func(f live.Feed) error {
			return livenodes.RunQuery(f, d.Nodes, livenodes.QueryRun{ID: g.ID,
				Graph: func() (*cognition.Graph, error) {
					in, ok := Running(f.Context(), d.Projections, f.Args().Principal.TenantID, app)
					if !ok {
						msg := fmt.Sprintf("the app %s is not installed", app)
						return nil, log.Code("app_not_installed").Public(msg).New(msg)
					}
					_, g, ok := in.Version.Query(name)
					if !ok {
						msg := fmt.Sprintf("the app %s has no query %s since version %d", app, name, in.Version.Version)
						return nil, log.Code("app_query_removed").Public(msg).New(msg)
					}
					return &g, nil
				},
				Watch: livenodes.Read{Projector: Projector, Key: TargetType + "." + app},
			})
		},
	}
}

// actionOf is the action an app's command needs: the manifest's
// (entity:task.update is record.update on task's records, under the app's
// path while it is installed), else its graph's.
func actionOf(c apps.Command, spec livenodes.CommandSpec) (string, func(features.Principal, live.Target) string) {
	entity, acts, ok := apps.EntityAction(c.Action)
	if !ok || len(acts) != 1 {
		return spec.Action, nil
	}
	return authz.Permission("record", acts[0]), func(p features.Principal, _ live.Target) string { return records.Resource(p, entity) }
}

func specOf[T any](raw json.RawMessage) (T, error) {
	var s T
	err := json.Unmarshal(raw, &s)
	return s, err
}

// Summary is one app in the app.installs query: what it puts on the live
// core, for the Apps section (forge 05d) and the chat.
type Summary struct {
	App         string            `json:"app"`
	Name        string            `json:"name"`
	Icon        string            `json:"icon,omitempty"`
	Description string            `json:"description,omitempty"`
	Status      string            `json:"status"`
	Version     int               `json:"version"`
	Commands    []Entry           `json:"commands"`
	Queries     []Entry           `json:"queries"`
	EditorKinds []apps.EditorKind `json:"editorKinds"`
	// Triggers are recorded; none runs until forge iteration 11 (forge 05b
	// question 4).
	Triggers    []apps.Trigger `json:"triggers"`
	InstalledBy uint64         `json:"installedBy,string"`
	InstalledAt time.Time      `json:"installedAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// Entry is one of an installed app's commands or queries, by the name it
// has on the live core.
type Entry struct {
	Name        string          `json:"name"` // <appId>.<name>
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"` // a command's args or a query's params, JSON Schema
	Action      string          `json:"action,omitempty"`
}

func summaryOf(in apps.Install) Summary {
	m := in.Version.Manifest
	s := Summary{App: in.App, Name: m.Name, Icon: m.Icon, Description: m.Description, Status: in.Status, Version: in.Version.Version,
		Commands: []Entry{}, Queries: []Entry{}, EditorKinds: m.EditorKinds, Triggers: m.Triggers,
		InstalledBy: in.InstalledBy, InstalledAt: in.InstalledAt, UpdatedAt: in.UpdatedAt}
	for _, c := range m.Commands {
		spec, _ := specOf[livenodes.CommandSpec](in.Version.Graphs[c.Graph].Spec)
		action, _ := actionOf(c, spec)
		s.Commands = append(s.Commands, Entry{Name: apps.Scoped(in.App, c.Type), Title: firstOf(c.Title, spec.Title),
			Description: firstOf(c.Description, spec.Description), Args: spec.Args, Action: action})
	}
	for _, q := range m.Queries {
		spec, _ := specOf[livenodes.QuerySpec](in.Version.Graphs[q.Graph].Spec)
		s.Queries = append(s.Queries, Entry{Name: apps.Scoped(in.App, q.Name), Title: spec.Title,
			Description: firstOf(q.Description, spec.Description), Args: spec.Params, Action: spec.Action})
	}
	return s
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func contributeQueries(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "app.installs", Source: "apps"}, Title: "Installed apps",
		Description: "The apps installed in this tenant (and those uninstalled, with their data kept), keyed app.<appId>: {app, name, icon, description, status, version, commands, queries, editorKinds, triggers, installedBy, installedAt, updatedAt}. Each command and query has the name it has on the live core, <appId>.<name>, its args' schema and the action it needs; send a command with live.command and read a query with live.query.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Action:      "app.read",
		Run: func(f live.Feed) error {
			_ = Follow(f.Args(), deps())
			entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, Projector, TargetType+".*")
			if err != nil {
				return err
			}
			all := map[string]Summary{}
			pos := live.Position{}
			started := false
			for e := range entries {
				if e == nil {
					started = true
					f.Snapshot(all, pos)
					continue
				}
				key := "app." + strings.TrimPrefix(e.Key, TargetType+".")
				pos = live.Position{e.Value.Stream: e.Value.Seq}
				var in apps.Install
				if e.Deleted || json.Unmarshal(e.Value.Value, &in) != nil {
					delete(all, key)
					if started {
						f.Change(live.Delta{Op: "remove", Key: key}, pos)
					}
					continue
				}
				s := summaryOf(in)
				all[key] = s
				if started {
					raw, _ := json.Marshal(s)
					f.Change(live.Delta{Op: "upsert", Key: key, Value: raw}, pos)
				}
			}
			return nil
		},
	})
}
