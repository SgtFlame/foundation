// Package manifests keeps app manifests editable by many people at once
// (forge 05a, documentation/Apps.md › Built in slice 31): an app's draft
// is an aggregate on the def stream, changed only by app.* commands and
// folded into the apps.app projection; a follower copies each app into
// the tenant database. The app.describe and app.list queries are what
// the Applications editor and the chat read.
package manifests

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// TargetType is an app's target type on the def stream.
const TargetType = "app"

// DraftProjector is each app's draft.
const DraftProjector = "apps.app"

// Draft is the apps.app projection's value: a manifest as it is being
// edited.
type Draft struct {
	Manifest apps.Manifest `json:"manifest"`
	Rev      int           `json:"rev"` // events folded; app.publish names the one its sender saw
	// Published is the last published version, 0 before the first, and
	// PublishedRev the rev it was published from: below Rev, the draft has
	// changes for the next version (forge iteration 5 question 5).
	Published    int              `json:"published"`
	PublishedRev int              `json:"publishedRev"`
	Versions     []VersionSummary `json:"versions"`
	// Seed are sample records the next version carries, created at
	// install into entity types that have none (forge iteration question 3).
	Seed      apps.Seed `json:"seed,omitempty"`
	CreatedBy uint64    `json:"createdBy,string"`
	UpdatedBy uint64    `json:"updatedBy,string"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Deleted hides the app, with its own parts, until app.restore (forge
	// 06b); it refuses other changes.
	Deleted bool `json:"deleted,omitempty"`
}

// Events an app's commands commit, beside each list's added, updated and
// removed (pageAdded, roleUpdated, …).
const (
	EventCreated     = "created"     // {manifest}
	EventInfoSet     = "infoSet"     // Info
	EventNavSet      = "navSet"      // {nav}
	EventSettingsSet = "settingsSet" // {settings}
	EventPublished   = "published"   // Published; the version itself is an event on its own target
	EventManifestSet = "manifestSet" // {manifest}: the whole manifest replaced
	EventSeedSet     = "seedSet"     // {seed}
	EventDeleted     = "deleted"     // {}
	EventRestored    = "restored"    // {}
)

// VersionTargetType is a published version's target type on the def
// stream, its id apps.VersionID (tasks-v3); its published event holds the
// whole apps.Version.
const VersionTargetType = "appVersion"

// VersionProjector keeps every published version, by its target.
const VersionProjector = "apps.version"

// Published is the app's published event: which version, from which rev.
type Published struct {
	Version int `json:"version"`
	Rev     int `json:"rev"`
}

// VersionSummary is one published version in the draft, newest last.
type VersionSummary struct {
	Version     int       `json:"version"`
	Rev         int       `json:"rev"`
	PublishedBy uint64    `json:"publishedBy,string"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Info changes an app's own fields; nil leaves them.
type Info struct {
	Name        *string        `json:"name,omitempty"`
	Icon        *string        `json:"icon,omitempty"`
	Description *string        `json:"description,omitempty"`
	Home        *string        `json:"home,omitempty"`
	Frame       *string        `json:"frame,omitempty"`
	Requires    *apps.Requires `json:"requires,omitempty"`
}

// List is one of a manifest's keyed lists. Every item is addressed by its
// own key (forge 05a question 4): app.add<Item> appends one, app.update<Item>
// merges the fields given into it (a JSON merge patch: null clears a
// field, objects such as rowRules merge per key, lists are replaced), and
// app.remove<Item> removes it, so two authors on different items never
// clash and two on one item merge per field.
type List struct {
	Item  string // page: the item in op and event names (addPage, pageAdded)
	Field string // pages: the manifest's field
	Key   string // route: the field naming an item; "" when items are ids
	Arg   string // the argument naming an item, when items are ids
	Title string // "page"
	// Props are the JSON Schema properties of an item, Required those an
	// added item needs beside its key.
	Props    string
	Required []string
	Example  string
	Update   string // an example update
	Describe string // what an item is, for a model
}

// Lists are the manifest's keyed lists.
var Lists = []List{
	{Item: "page", Field: "pages", Key: "route", Title: "page", Required: []string{"prefab"},
		Props:    `"route":{"type":"string","description":"/mine, or /task/:id: its params become the prefab's props"},"prefab":{"type":"string","description":"The prefab's id"},"title":{"type":"string","description":"The page's title, in its header and tab"},"description":{"type":"string","description":"What the page shows and who it is for, for a model"}`,
		Example:  `{"route":"/mine","prefab":"myTasks","title":"My tasks","description":"The open tasks assigned to the user"}`,
		Update:   `{"route":"/mine","title":"Mine"}`,
		Describe: "a route to a prefab; the route's params become the prefab's props. Renaming a route (rename) renames it in the navigation, home and editor kinds too"},
	{Item: "editorKind", Field: "editorKinds", Key: "kind", Title: "editor kind", Required: []string{"page", "title"},
		Props:    `"kind":{"type":"string","description":"The kind's name, unique in the app, such as task"},"page":{"type":"string","description":"A page's route"},"title":{"type":"string","description":"A template over the page's data, such as {record.title}"},"icon":{"type":"string","description":"A Material icon name"},"subject":{"type":"object","description":"What the chat's workbench context says the tab shows: {type, label}, label a template"}`,
		Example:  `{"kind":"task","page":"/task/:id","title":"{record.title}","subject":{"type":"task","label":"{record.title}"}}`,
		Update:   `{"kind":"task","icon":"checklist"}`,
		Describe: "a page that opens as its own top-level workbench tab"},
	{Item: "command", Field: "commands", Key: "type", Title: "command", Required: []string{"graph"},
		Props:    `"type":{"type":"string","description":"A dotted name, such as task.reassign"},"graph":{"type":"string","description":"The command graph that decides it"},"action":{"type":"string","description":"The action a role needs to send it, such as entity:task.update"},"title":{"type":"string","description":"What people call it"},"description":{"type":"string","description":"What it does, for a model"}`,
		Example:  `{"type":"task.reassign","graph":"tasks-reassign","action":"entity:task.update"}`,
		Update:   `{"type":"task.reassign","action":"entity:task.reassign"}`,
		Describe: "one of the app's own commands, beyond the record.* ones every entity type has"},
	{Item: "query", Field: "queries", Key: "name", Title: "query", Required: []string{"graph"},
		Props:    `"name":{"type":"string","description":"The name pages bind to, as query:<name>"},"graph":{"type":"string","description":"The data.query graph that answers it"},"description":{"type":"string","description":"What it returns, for a model"}`,
		Example:  `{"name":"myTasks","graph":"tasks-myTasks"}`,
		Update:   `{"name":"myTasks","description":"The open tasks assigned to me"}`,
		Describe: "a named data.query graph pages bind to"},
	{Item: "role", Field: "roles", Key: "id", Title: "role", Required: []string{"actions"},
		Props:    `"id":{"type":"string","description":"The role's id, unique in the app, such as member"},"title":{"type":"string","description":"What people call it, such as Member"},"description":{"type":"string","description":"Who it is for and what it may do"},"actions":{"type":"array","items":{"type":"string"},"description":"entity:<id>.<read|create|update|delete|*> on the app's own entity types, such as entity:task.read or entity:task.*"},"rowRules":{"type":"object","description":"entity type → a rule over record and principal, such as record.assignee == principal.user, that every record the role reads or changes must pass; null clears one"},"installer":{"type":"boolean","description":"Whoever installs the app gets this role; the first role when none is marked"}`,
		Example:  `{"id":"member","title":"Member","actions":["entity:task.read","entity:task.create"],"rowRules":{"task":"record.assignee == principal.user"}}`,
		Update:   `{"id":"member","rowRules":{"task":"record.assignee == principal.user || record.public"}}`,
		Describe: "a role, made a custom role in each tenant the app is installed in"},
	{Item: "trigger", Field: "triggers", Key: "id", Title: "trigger",
		Props:    `"id":{"type":"string","description":"The trigger's id, unique in the app"},"title":{"type":"string","description":"What people call it"},"description":{"type":"string","description":"When it fires, for a model"},"graph":{"type":"string","description":"The app's own app.trigger graph it runs; its event node (On record changed, On event, On schedule) says when"},"queue":{"type":"string","description":"The job queue its runs go to; default when left out"},"config":{"type":"object","description":"Settings for its graph"}`,
		Example:  `{"id":"taskDone","title":"A task is done","graph":"tasks-taskDone"}`,
		Update:   `{"id":"taskDone","description":"Fires when a task is marked done"}`,
		Describe: "background work the installed app runs, as its own principal, each time its graph's event node fires: an event, a record change or a schedule (a trigger without a graph is only declared)"},
	{Item: "entity", Field: "entities", Arg: "entity", Title: "entity type",
		Props:    `"entity":{"type":"string","description":"The entity type's id"}`,
		Example:  `{"entity":"task"}`,
		Describe: "an entity type the app uses: its own (owner app:<appId>), the tenant's shared ones, or one of an app it requires"},
}

// ListOf finds a list by its item.
func ListOf(item string) (List, bool) {
	i := slices.IndexFunc(Lists, func(l List) bool { return l.Item == item })
	if i < 0 {
		return List{}, false
	}
	return Lists[i], true
}

// keyProp is the schema of a list's key argument, from its Props.
func (l List) keyProp(key string) string {
	var props map[string]json.RawMessage
	_ = json.Unmarshal([]byte("{"+l.Props+"}"), &props)
	if p, ok := props[key]; ok {
		return string(p)
	}
	return `{"type":"string"}`
}

// Event names of a list.
func (l List) Added() string   { return l.Item + "Added" }
func (l List) Updated() string { return l.Item + "Updated" }
func (l List) Removed() string { return l.Item + "Removed" }

// Added is a list's added event: the item, at index (the end when < 0).
type Added struct {
	Item  any `json:"item"`
	Index int `json:"index"`
}

// Updated is a list's updated event: a merge patch on the item at key.
type Updated struct {
	Key   string         `json:"key"`
	Patch map[string]any `json:"patch"`
}

// keyOf is an item's key.
func (l List) keyOf(item any) string {
	if l.Key == "" {
		s, _ := item.(string)
		return s
	}
	m, _ := item.(map[string]any)
	s, _ := m[l.Key].(string)
	return s
}

// items is the list as JSON values, from a manifest as a JSON object.
func (l List) items(doc map[string]any) []any {
	items, _ := doc[l.Field].([]any)
	return items
}

func (l List) find(doc map[string]any, key string) int {
	return slices.IndexFunc(l.items(doc), func(it any) bool { return l.keyOf(it) == key })
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// asDoc is a manifest as a JSON object, which list events change.
func asDoc(m apps.Manifest) map[string]any {
	raw, _ := json.Marshal(m)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	return doc
}

func fromDoc(doc map[string]any) (apps.Manifest, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return apps.Manifest{}, err
	}
	return decode[apps.Manifest](raw)
}

// Merge applies a JSON merge patch (RFC 7386) to a value, never changing
// the value it is given.
func Merge(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	t, _ := target.(map[string]any)
	out := maps.Clone(t)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range p {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = Merge(out[k], v)
		}
	}
	return out
}

// applyList folds one list event into a manifest.
func applyList(m *apps.Manifest, l List, typ string, data json.RawMessage) error {
	doc := asDoc(*m)
	items := slices.Clone(l.items(doc))
	switch typ {
	case l.Added():
		a, err := decode[Added](data)
		if err != nil {
			return err
		}
		if l.find(doc, l.keyOf(a.Item)) >= 0 {
			return fmt.Errorf("there is already a %s %s", l.Title, l.keyOf(a.Item))
		}
		if a.Index < 0 || a.Index > len(items) {
			a.Index = len(items)
		}
		items = slices.Insert(items, a.Index, a.Item)
	case l.Updated():
		u, err := decode[Updated](data)
		if err != nil {
			return err
		}
		i := l.find(doc, u.Key)
		if i < 0 {
			return fmt.Errorf("there is no %s %s", l.Title, u.Key)
		}
		items[i] = Merge(items[i], u.Patch)
		if to := l.keyOf(items[i]); to != u.Key && l.Item == "page" {
			renameRoute(doc, u.Key, to)
		}
	case l.Removed():
		k, err := decode[struct{ Key string }](data)
		if err != nil {
			return err
		}
		i := l.find(doc, k.Key)
		if i < 0 {
			return fmt.Errorf("there is no %s %s", l.Title, k.Key)
		}
		items = slices.Delete(items, i, i+1)
	default:
		return fmt.Errorf("unknown event %s", typ)
	}
	doc[l.Field] = items
	out, err := fromDoc(doc)
	if err != nil {
		return err
	}
	*m = out
	return nil
}

// renameRoute follows a page's new route in what names it: the home, the
// navigation and the editor kinds.
func renameRoute(doc map[string]any, from, to string) {
	if doc["home"] == from {
		doc["home"] = to
	}
	for field, key := range map[string]string{"nav": "route", "editorKinds": "page"} {
		items, _ := doc[field].([]any)
		for _, it := range items {
			if obj, ok := it.(map[string]any); ok && obj[key] == from {
				obj[key] = to
			}
		}
	}
}

// listOfEvent finds the list an event belongs to.
func listOfEvent(typ string) (List, bool) {
	for _, l := range Lists {
		if typ == l.Added() || typ == l.Updated() || typ == l.Removed() {
			return l, true
		}
	}
	return List{}, false
}

// Apply folds one event into a draft. Commands also apply the events they
// decide to a copy, and check the result.
func Apply(d *Draft, e live.Event) error {
	m := &d.Manifest
	var err error
	switch e.Type {
	case EventCreated:
		var v struct{ Manifest apps.Manifest }
		if v, err = decode[struct{ Manifest apps.Manifest }](e.Data); err == nil {
			*m = v.Manifest
			d.CreatedBy = e.UserID
		}
	case EventInfoSet:
		var in Info
		if in, err = decode[Info](e.Data); err == nil {
			set := func(dst *string, v *string) {
				if v != nil {
					*dst = *v
				}
			}
			set(&m.Name, in.Name)
			set(&m.Icon, in.Icon)
			set(&m.Description, in.Description)
			set(&m.Home, in.Home)
			set(&m.Frame, in.Frame)
			if in.Requires != nil {
				m.Requires = *in.Requires
			}
		}
	case EventManifestSet:
		var v struct{ Manifest apps.Manifest }
		if v, err = decode[struct{ Manifest apps.Manifest }](e.Data); err == nil {
			*m = v.Manifest
		}
	case EventSeedSet:
		var v struct{ Seed apps.Seed }
		if v, err = decode[struct{ Seed apps.Seed }](e.Data); err == nil {
			d.Seed = v.Seed
		}
	case EventNavSet:
		var in struct{ Nav []apps.NavItem }
		if in, err = decode[struct{ Nav []apps.NavItem }](e.Data); err == nil {
			m.Nav = in.Nav
		}
	case EventPublished:
		var p Published
		if p, err = decode[Published](e.Data); err == nil {
			d.Published = p.Version
			// Published at the rev before this event: nothing waits for the
			// next version; at an earlier rev, the changes since do
			d.PublishedRev = p.Rev
			if p.Rev == d.Rev {
				d.PublishedRev = d.Rev + 1
			}
			d.Versions = append(d.Versions, VersionSummary{Version: p.Version, Rev: p.Rev, PublishedBy: e.UserID, PublishedAt: e.Time})
		}
	case EventDeleted, EventRestored:
		d.Deleted = e.Type == EventDeleted
	case EventSettingsSet:
		var in struct{ Settings apps.Settings }
		if in, err = decode[struct{ Settings apps.Settings }](e.Data); err == nil {
			m.Settings = in.Settings
		}
	default:
		l, ok := listOfEvent(e.Type)
		if !ok {
			return fmt.Errorf("unknown event %s", e.Type)
		}
		err = applyList(m, l, e.Type, e.Data)
	}
	if err != nil {
		return err
	}
	d.Rev++
	d.UpdatedBy, d.UpdatedAt = e.UserID, e.Time
	m.ID, m.Version = e.Target.ID, d.Published
	apps.Normalize(m)
	if d.Versions == nil {
		d.Versions = []VersionSummary{}
	}
	return nil
}

// fold is the apps.app projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var d Draft
	if f.State != nil {
		if err := json.Unmarshal(f.State, &d); err != nil {
			return nil, err
		}
	}
	if err := Apply(&d, f.Event); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// Keys are a list's item keys, in order.
func Keys(m apps.Manifest, item string) []string {
	l, ok := ListOf(item)
	if !ok {
		return nil
	}
	out := []string{}
	for _, it := range l.items(asDoc(m)) {
		out = append(out, l.keyOf(it))
	}
	return out
}

// title is "Page" for "page".
func title(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
