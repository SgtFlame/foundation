// Package apps is what an application is (forge 05, documentation/Apps.md):
// a manifest of pages, navigation, editor kinds, entity types, commands,
// queries, roles, settings and triggers, checked whole by CheckManifest.
// Its draft, changed only by app.* commands, is apps/manifests; the tools
// that read it are apps/tools.
package apps

import (
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the apps' logging and error builder.
var Log = logging.In("apps")

// Manifest is an application: what it shows, what it stores and who may
// do what in it. Its parts (prefabs, graphs, entity types) live where they
// always do, with the owner app:<id>; the manifest names them by id.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	// Home is the route the app opens on; empty is its first page's.
	Home string `json:"home,omitempty"`
	// Frame is the prefab that wraps every page (prefab.<id>); empty is
	// the default frame, a navigation rail and a header.
	Frame       string       `json:"frame,omitempty"`
	Pages       []Page       `json:"pages"`
	Nav         []NavItem    `json:"nav"`
	EditorKinds []EditorKind `json:"editorKinds"`
	// Entities are the entity types the app uses: its own (owner
	// app:<id>), the tenant's shared ones, and those of the apps it
	// requires.
	Entities []string  `json:"entities"`
	Commands []Command `json:"commands"`
	Queries  []Query   `json:"queries"`
	Roles    []Role    `json:"roles"`
	Settings Settings  `json:"settings"`
	Triggers []Trigger `json:"triggers"`
	Requires Requires  `json:"requires"`
	Version  int       `json:"version"` // the published version; 0 in a draft never published
}

// DefaultFrame is the prefab an app that names no frame is framed by:
// Forge's starter App frame (forge 05d question 2). Without it, a plain
// frame drawn by the runtime.
const DefaultFrame = "appFrame"

// FrameSlot is the frame's slot the current page goes in.
const FrameSlot = "page"

// Page is a route to a prefab. The route's params (/task/:id) become the
// prefab's props.
type Page struct {
	Route  string `json:"route"`
	Prefab string `json:"prefab"`
	Title  string `json:"title,omitempty"`
}

// NavItem is one entry of the frame's navigation.
type NavItem struct {
	Title string `json:"title"`
	Route string `json:"route"`
	Icon  string `json:"icon,omitempty"`
}

// EditorKind is a page that opens as its own top-level workbench tab. Its
// title and subject are templates over the page's data ({record.title}).
type EditorKind struct {
	Kind    string   `json:"kind"`
	Page    string   `json:"page"` // a page's route
	Title   string   `json:"title"`
	Icon    string   `json:"icon,omitempty"`
	Subject *Subject `json:"subject,omitempty"`
}

// Subject is what the chat's workbench context says an editor-kind tab
// shows.
type Subject struct {
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

// Command is one of the app's own commands, beyond the record.* ones every
// entity type has: the command graph that decides it, and the action a
// role needs to send it.
type Command struct {
	Type        string `json:"type"`
	Graph       string `json:"graph"`
	Action      string `json:"action,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// Query is one of the app's named data.query graphs.
type Query struct {
	Name        string `json:"name"`
	Graph       string `json:"graph"`
	Description string `json:"description,omitempty"`
}

// Role becomes a custom role in a tenant the app is installed in (forge
// 05c). Its row rules are per entity type, expressions over record and
// principal.
type Role struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Actions are entity:<id>.<read|create|update|delete|*> on the app's
	// own entity types.
	Actions  []string          `json:"actions"`
	RowRules map[string]string `json:"rowRules,omitempty"`
	// Installer marks the role whoever installs the app is given; without
	// one, it is the first role.
	Installer bool `json:"installer,omitempty"`
}

// Settings become the configuration section app.<id> when the app is
// installed (forge 05c).
type Settings struct {
	Schema   map[string]any `json:"schema,omitempty"` // a JSON Schema object
	Defaults map[string]any `json:"defaults,omitempty"`
}

// Trigger is declared here and filled in by forge iteration 11.
type Trigger struct {
	ID          string         `json:"id"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
}

// Requires is what must be in a tenant before the app installs there.
type Requires struct {
	// Primitives are component ids (mui.table) or families (mui).
	Primitives []string `json:"primitives,omitempty"`
	Apps       []string `json:"apps,omitempty"`
}

// Normalize gives a manifest's lists empty slices rather than null.
func Normalize(m *Manifest) {
	if m.Pages == nil {
		m.Pages = []Page{}
	}
	if m.Nav == nil {
		m.Nav = []NavItem{}
	}
	if m.EditorKinds == nil {
		m.EditorKinds = []EditorKind{}
	}
	if m.Entities == nil {
		m.Entities = []string{}
	}
	if m.Commands == nil {
		m.Commands = []Command{}
	}
	if m.Queries == nil {
		m.Queries = []Query{}
	}
	if m.Roles == nil {
		m.Roles = []Role{}
	}
	for i := range m.Roles {
		if m.Roles[i].Actions == nil {
			m.Roles[i].Actions = []string{}
		}
	}
	if m.Triggers == nil {
		m.Triggers = []Trigger{}
	}
}

// Owner is the owner an app's parts carry.
func Owner(appID string) string { return "app:" + appID }
