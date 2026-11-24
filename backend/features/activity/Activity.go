// Package activity is the history of what a person does by hand (forge
// 15c, documentation/Activity.md › Built in forge 15c): the tabs they
// focus, their clicks on app pages, the record and app commands they send
// and their definition changes, kept briefly in a per-tenant JetStream
// stream with one subject per person (ACT_<tenant>, act.<tenant>.<userId>)
// and read only by that person, grouped by request into gestures.
//
// People's gestures alone are recorded: agents, apps, service principals
// and the chat's tool calls record nothing, since their runs and rounds
// keep their own records. A click records the handler, the node's key
// path and its visible label, never the payload; a command records the
// changed fields with the values it holds, but never long text, Markdown,
// rich text or files.
package activity

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("activity")

// The kinds of entries.
const (
	KindFocus   = "focus"   // a tab opened or focused (activity.focus)
	KindUI      = "ui"      // an event on an app page
	KindCommand = "command" // a record or app command's event
	KindOp      = "op"      // a definition change, an op of the operation log
	KindDismiss = "dismiss" // gestures the person dismissed (activity.dismiss)
	KindClear   = "clear"   // everything before it was cleared (activity.clear)
)

// StreamName is a tenant's activity stream.
func StreamName(tenant uint64) string { return fmt.Sprintf("ACT_%d", tenant) }

// Subject is one person's subject in their tenant's stream.
func Subject(tenant, user uint64) string { return fmt.Sprintf("act.%d.%d", tenant, user) }

// Subjects is everything a tenant's stream holds.
func Subjects(tenant uint64) string { return fmt.Sprintf("act.%d.*", tenant) }

// Thing is what a gesture was about: the workbench subject a tab reported
// (type, id, label, details), or a page's subject.
type Thing struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Label   string            `json:"label,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// Field is a field a command changed: its id and name, and the new value
// as the command holds it, unless the value is content the history never
// copies (Omitted: long text, Markdown, rich text, files, JSON).
type Field struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Value   any    `json:"value,omitempty"`
	Omitted bool   `json:"omitted,omitempty"`
}

// Entry is one thing recorded. Which fields are set depends on Kind.
type Entry struct {
	ID      string    `json:"id"` // its message id: the frame's request, the event's stream and sequence
	Kind    string    `json:"kind"`
	Request string    `json:"request,omitempty"` // entries of one request are one gesture
	Time    time.Time `json:"time"`
	App     string    `json:"app,omitempty"`
	// Subject is what the tab showed: the focused one's, or the page's
	Subject *Thing `json:"subject,omitempty"`
	// ui: the page, its tab's title, the handler, the component event, the
	// node's key path, type and visible label
	Page    string   `json:"page,omitempty"`
	Title   string   `json:"title,omitempty"`
	Handler string   `json:"handler,omitempty"`
	Event   string   `json:"event,omitempty"`
	Path    []string `json:"path,omitempty"`
	Node    string   `json:"node,omitempty"`
	Label   string   `json:"label,omitempty"`
	// command and op: the command type, its title, the target's entity
	// type (and its name), the target and its record's title, the event
	// (created, updated, deleted…) and the changed fields
	Command      string  `json:"command,omitempty"`
	CommandTitle string  `json:"commandTitle,omitempty"`
	Entity       string  `json:"entity,omitempty"`
	EntityName   string  `json:"entityName,omitempty"`
	Target       string  `json:"target,omitempty"`
	RecordTitle  string  `json:"recordTitle,omitempty"`
	Change       string  `json:"change,omitempty"`
	Fields       []Field `json:"fields,omitempty"`
	// dismiss: the gestures dismissed
	IDs []string `json:"ids,omitempty"`
	// Summary is the entry in plain words: opened "Refund for the second
	// charge?", clicked "Billing", Label thread "Refund…" (Labels: Billing)
	Summary string `json:"summary,omitempty"`
}

// Gesture is one request's entries: a click and the commands it caused, a
// tab focused, a definition change.
type Gesture struct {
	ID      string    `json:"id"` // the request
	Start   time.Time `json:"start"`
	Time    time.Time `json:"time"` // its last entry's
	App     string    `json:"app,omitempty"`
	Subject *Thing    `json:"subject,omitempty"`
	Summary string    `json:"summary"`
	// Repeats is how many more times the same event on the same node came
	// right after, merged into this one (typing in a search field)
	Repeats   int     `json:"repeats,omitempty"`
	Dismissed bool    `json:"dismissed,omitempty"`
	Entries   []Entry `json:"entries"`
}

// Activity is activity.mine's result: the caller's gestures, newest last,
// and whether recording is on for them.
type Activity struct {
	Recording bool      `json:"recording"`
	Gestures  []Gesture `json:"gestures"`
}

// SectionID is the activity settings section.
const SectionID = "activity"

// Settings is the activity section: whether a person's activity is
// recorded, and how much of it is kept.
type Settings struct {
	Record    bool `json:"record"`
	KeepHours int  `json:"keepHours"`
	KeepCount int  `json:"keepCount"`
}

// Defaults are the section's defaults: recording on (forge 15c question
// 11), four hours, 300 entries.
var Defaults = Settings{Record: true, KeepHours: 4, KeepCount: 300}

// The stream's own limits, the settings' maximums: no setting keeps more.
const (
	MaxHours = 24
	MaxCount = 1000
)

// Keep is how long entries are kept.
func (s Settings) Keep() time.Duration {
	h := s.KeepHours
	if h <= 0 {
		h = Defaults.KeepHours
	}
	return time.Duration(min(h, MaxHours)) * hour
}

// Count is how many entries are kept.
func (s Settings) Count() int {
	n := s.KeepCount
	if n <= 0 {
		n = Defaults.KeepCount
	}
	return min(n, MaxCount)
}

// hour is an hour of keepHours; tests shorten it.
var hour = time.Hour

// ContributeSettings registers the activity section.
func ContributeSettings(reg *extensions.Registry) {
	raw, _ := json.Marshal(Defaults)
	config.Sections.Register(reg, config.Section{
		Descriptor:  extensions.Descriptor{ID: SectionID, Order: 450, Source: "activity"},
		Title:       "Recent activity",
		Description: "Keeps a short history of what you do by hand (tabs you open, clicks on app pages, records you change) so the chat can see what you just did and help automate it. Only you can read it; it is never shown to administrators or agents. Turning it off also clears what is kept.",
		Category:    []string{"Chat"},
		Scopes:      []string{"tenant", "user"},
		Schema:      json.RawMessage(schema),
		Defaults:    raw,
		Version:     1,
	})
}

const schema = `{
  "type": "object",
  "properties": {
    "record": {"type": "boolean", "title": "Record my recent activity"},
    "keepHours": {"type": "integer", "title": "Keep it for", "min": 1, "max": 24, "unit": "hours"},
    "keepCount": {"type": "integer", "title": "Keep at most", "min": 10, "max": 1000, "unit": "entries"}
  }
}`

// ContributeAccess declares the activity resource type: a person's own
// history, which they alone read and change.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "activity", Source: "activity", Order: 45}, Title: "Recent activity", Parent: "tenant", Owned: true,
		Actions: []authz.Action{
			{Name: "read", Title: "See my recent activity", Class: authz.ClassSelf},
			{Name: "write", Title: "Record, dismiss and clear my recent activity", Class: authz.ClassSelf},
		},
	})
}
