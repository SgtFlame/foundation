package apps

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Version is a published app version (forge 05b): its manifest, frozen,
// and the version of every part it uses (forge 05b question 1), so a part
// published again reaches the app's users only through a new app version
// and an upgrade.
type Version struct {
	App      string   `json:"app"`
	Version  int      `json:"version"`
	Rev      int      `json:"rev"` // the draft's rev it was published from
	Manifest Manifest `json:"manifest"`
	// Prefabs are the published versions of the prefabs its pages and frame
	// render, and of those they render in turn.
	Prefabs map[string]int `json:"prefabs"`
	// Entities are the published versions of its entity types.
	Entities map[string]int `json:"entities"`
	// Owned are the entity types the app owns, whose records are under its
	// path while it is installed (forge 05c question 3).
	Owned []string `json:"owned,omitempty"`
	// Graphs are copies of its graphs as they were published: its
	// commands' and queries', and those its prefabs name. A graph keeps
	// only its last published version, so the app keeps its own.
	Graphs map[string]cognition.Graph `json:"graphs"`
	// Seed are sample records, created at install into the app's own
	// entity types that have none (forge iteration question 3).
	Seed        Seed      `json:"seed,omitempty"`
	PublishedBy uint64    `json:"publishedBy,string"`
	PublishedAt time.Time `json:"publishedAt"`
}

// VersionID is a version's target id on the def stream: tasks-v3.
func VersionID(app string, version int) string { return app + "-v" + strconv.Itoa(version) }

// Command is one of the version's commands with its pinned graph.
func (v Version) Command(typ string) (Command, cognition.Graph, bool) {
	i := slices.IndexFunc(v.Manifest.Commands, func(c Command) bool { return c.Type == typ })
	if i < 0 {
		return Command{}, cognition.Graph{}, false
	}
	g, ok := v.Graphs[v.Manifest.Commands[i].Graph]
	return v.Manifest.Commands[i], g, ok
}

// Query is one of the version's queries with its pinned graph.
func (v Version) Query(name string) (Query, cognition.Graph, bool) {
	i := slices.IndexFunc(v.Manifest.Queries, func(q Query) bool { return q.Name == name })
	if i < 0 {
		return Query{}, cognition.Graph{}, false
	}
	g, ok := v.Graphs[v.Manifest.Queries[i].Graph]
	return v.Manifest.Queries[i], g, ok
}

// Scoped is an app's command type or query name on the live core:
// <appId>.<name> (forge 05b question 3). Inside the app, in its manifest
// and graphs, it is the name alone.
func Scoped(app, name string) string { return app + "." + name }

// Unscoped splits a scoped name into the app and the name inside it.
func Unscoped(typ string) (app, name string, ok bool) {
	app, name, ok = strings.Cut(typ, ".")
	return app, name, ok && app != "" && name != ""
}

// DraftPrefix starts the live name of a command or query of an app's
// draft run (forge 05f question 3): draft.<appId>.<name>, decided and
// answered by the draft's graphs. No app may have the id draft.
const DraftPrefix = "draft."

// DraftScoped is a draft run's name for one of the app's own commands or
// queries.
func DraftScoped(app, name string) string { return DraftPrefix + Scoped(app, name) }

// DraftUnscoped splits a draft run's name into the app and the name
// inside it.
func DraftUnscoped(typ string) (app, name string, ok bool) {
	rest, ok := strings.CutPrefix(typ, DraftPrefix)
	if !ok {
		return "", "", false
	}
	return Unscoped(rest)
}

// Install is an app installed in a tenant (forge 05b question 2: once per
// tenant, upgraded in place): the version it runs, whole, so every reader
// sees one version or the next, never a mix.
type Install struct {
	App         string    `json:"app"`
	Status      string    `json:"status"` // InstallInstalled or InstallUninstalled
	Version     Version   `json:"version"`
	InstalledBy uint64    `json:"installedBy,string"`
	InstalledAt time.Time `json:"installedAt"`
	UpdatedBy   uint64    `json:"updatedBy,string"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Install statuses. An uninstalled app keeps its data until a purge.
const (
	InstallInstalled   = "installed"
	InstallUninstalled = "uninstalled"
)

// Installed reports whether the app runs.
func (i Install) Installed() bool { return i.Status == InstallInstalled }

// Missing is what a tenant lacks for an app's requires, each a problem
// at requires: primitives not in the catalog (a component id, or a
// family with no component) and apps not installed there.
func Missing(r Requires, catalog map[string]ui.Component, installed func(app string) bool) []live.Problem {
	var out []live.Problem
	for _, p := range r.Primitives {
		if !HasPrimitive(catalog, p) {
			out = append(out, live.Problem{Code: "app_requires", Path: "requires.primitives[" + p + "]",
				Message: fmt.Sprintf("the app needs the primitive %s, which this server does not have", p), Fix: "install a server version that has it, or drop it from the app's requires"})
		}
	}
	for _, a := range r.Apps {
		if !installed(a) {
			out = append(out, live.Problem{Code: "app_requires", Path: "requires.apps[" + a + "]",
				Message: fmt.Sprintf("the app needs the app %s, which is not installed here", a), Fix: "install " + a + " first with app.install"})
		}
	}
	return out
}
