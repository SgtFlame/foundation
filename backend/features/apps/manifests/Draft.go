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
	Manifest  apps.Manifest `json:"manifest"`
	Rev       int           `json:"rev"` // events folded
	CreatedBy uint64        `json:"createdBy,string"`
	UpdatedBy uint64        `json:"updatedBy,string"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Events an app's commands commit, beside each list's added, updated and
// removed (pageAdded, roleUpdated, …).
const (
	EventCreated     = "created"     // {manifest}
	EventInfoSet     = "infoSet"     // Info
	EventNavSet      = "navSet"      // {nav}
	EventSettingsSet = "settingsSet" // {settings}
)

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
		Props:    `"route":{"type":"string","description":"/mine, or /task/:id: its params become the prefab's props"},"prefab":{"type":"string","description":"The prefab's id"},"title":{"type":"string"}`,
		Example:  `{"route":"/mine","prefab":"myTasks","title":"My tasks"}`,
		Update:   `{"route":"/mine","title":"Mine"}`,
		Describe: "a route to a prefab; the route's params become the prefab's props. Renaming a route (rename) renames it in the navigation, home and editor kinds too"},
	{Item: "editorKind", Field: "editorKinds", Key: "kind", Title: "editor kind", Required: []string{"page", "title"},
		Props:    `"kind":{"type":"string"},"page":{"type":"string","description":"A page's route"},"title":{"type":"string","description":"A template over the page's data, such as {record.title}"},"icon":{"type":"string"},"subject":{"type":"object","description":"What the chat's workbench context says the tab shows: {type, label}, label a template"}`,
		Example:  `{"kind":"task","page":"/task/:id","title":"{record.title}","subject":{"type":"task","label":"{record.title}"}}`,
		Update:   `{"kind":"task","icon":"checklist"}`,
		Describe: "a page that opens as its own top-level workbench tab"},
	{Item: "command", Field: "commands", Key: "type", Title: "command", Required: []string{"graph"},
		Props:    `"type":{"type":"string","description":"A dotted name, such as task.reassign"},"graph":{"type":"string","description":"The command graph that decides it"},"action":{"type":"string","description":"The action a role needs to send it, such as entity:task.update"},"title":{"type":"string"},"description":{"type":"string","description":"What it does, for a model"}`,
		Example:  `{"type":"task.reassign","graph":"tasks-reassign","action":"entity:task.update"}`,
		Update:   `{"type":"task.reassign","action":"entity:task.reassign"}`,
		Describe: "one of the app's own commands, beyond the record.* ones every entity type has"},
	{Item: "query", Field: "queries", Key: "name", Title: "query", Required: []string{"graph"},
		Props:    `"name":{"type":"string"},"graph":{"type":"string","description":"The data.query graph that answers it"},"description":{"type":"string"}`,
		Example:  `{"name":"myTasks","graph":"tasks-myTasks"}`,
		Update:   `{"name":"myTasks","description":"The open tasks assigned to me"}`,
		Describe: "a named data.query graph pages bind to"},
	{Item: "role", Field: "roles", Key: "id", Title: "role", Required: []string{"actions"},
		Props:    `"id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"actions":{"type":"array","items":{"type":"string"},"description":"<resource>.<action>, such as entity:task.read or entity:task.*"},"rowRules":{"type":"object","description":"entity type → a rule over record and principal, such as record.assignee == principal.user; null clears one"}`,
		Example:  `{"id":"member","title":"Member","actions":["entity:task.read","entity:task.create"],"rowRules":{"task":"record.assignee == principal.user"}}`,
		Update:   `{"id":"member","rowRules":{"task":"record.assignee == principal.user || record.public"}}`,
		Describe: "a role, made a custom role in each tenant the app is installed in"},
	{Item: "trigger", Field: "triggers", Key: "id", Title: "trigger",
		Props:    `"id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"config":{"type":"object"}`,
		Example:  `{"id":"taskDone","title":"A task is done"}`,
		Update:   `{"id":"taskDone","description":"Fires when a task is marked done"}`,
		Describe: "a trigger, declared now and run once triggers are built (forge iteration 11)"},
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
	case EventNavSet:
		var in struct{ Nav []apps.NavItem }
		if in, err = decode[struct{ Nav []apps.NavItem }](e.Data); err == nil {
			m.Nav = in.Nav
		}
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
	m.ID = e.Target.ID
	apps.Normalize(m)
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
