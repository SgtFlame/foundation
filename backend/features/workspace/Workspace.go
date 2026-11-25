// Package workspace keeps a person's workspaces (forge 16a,
// documentation/Workspaces.md): contexts a person switches between, each
// holding the open editors and their layout (16b), the active Volumes and
// Chronicle entries (16c) and the chats that belong to it (16d).
//
// A workspace is CQRS state on the live core, on its own area (stream
// EVT_<tenant>_ws), changed only by the workspace.* commands and read by
// the live workspace.list query:
//   - one person's workspaces are theirs alone: the target type is
//     ws_<userId>, made from the principal (live.Command.TypeFor), so a
//     command never reaches another person's workspace, an administrator's
//     neither, and the query watches the pattern ws_<userId>.* alone;
//   - Default is made by the first read of a person's workspaces, with the
//     fixed id "default" and an expected-sequence commit, so two processes
//     reading at once make one;
//   - workspace.update {version} is how 16b and 16c save the layout and
//     context: a stale version is refused (stale_workspace) and the client
//     merges and retries.
//
// Workspaces are not definitions, so their commands are not ops of the
// operation log, and they are not a person's activity.
package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
)

const (
	// Area is the workspaces' area on the live core: EVT_<tenant>_ws.
	Area live.Area = "ws"
	// Projector folds each workspace, keyed ws_<userId>.<id>.
	Projector = "workspace.workspaces"
	// DefaultID is every person's Default workspace's id.
	DefaultID = "default"
	// DefaultName is what Default is called until it is renamed.
	DefaultName = "Default"
	// MaxName is the longest name a workspace may have, in characters.
	MaxName = 60
	// MaxPart is the most bytes a workspace's layout or context may hold
	// (forge 16b): a layout of a few dozen tabs is a few kilobytes.
	MaxPart = 256 << 10
	source  = "workspace"
)

// Events of a workspace.
const (
	EventAdded   = "added"   // {workspace}: made or duplicated
	EventRenamed = "renamed" // {name}
	EventMoved   = "moved"   // {position}
	EventUpdated = "updated" // {layout?, context?}
	EventDeleted = "deleted" // {}
)

// TypeOf is the target type of user's workspaces.
func TypeOf(user uint64) string { return "ws_" + strconv.FormatUint(user, 10) }

func typeFor(p features.Principal) string { return TypeOf(p.UserID) }

// Workspace is one workspace as the projection keeps it.
type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault,omitempty"`
	Position  int    `json:"position"`
	// Layout is the open editors and their layout (written by 16b), and
	// Context the active Volumes and Chronicle entries (16c): JSON the
	// parts that write them define.
	Layout  json.RawMessage `json:"layout,omitempty"`
	Context json.RawMessage `json:"context,omitempty"`
	// Version is raised by every change: workspace.update names the one it
	// read.
	Version   uint64    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Deps are what the workspaces need, made after the registry.
type Deps struct {
	Store live.Store
	Bus   *bus.Bus
}

// Resource is where a person's workspaces are checked: their own scope.
func Resource(p features.Principal) string {
	return authz.Tenant(p) + "/" + authz.Resource("user", p.UserID) + "/workspaces"
}

// ContributeAccess declares the workspace resource type: a person's own,
// which they alone read and change.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "workspace", Source: source, Order: 46}, Title: "Workspaces", Parent: "tenant", Owned: true,
		Actions: []authz.Action{
			{Name: "read", Title: "See my workspaces", Class: authz.ClassSelf},
			{Name: "write", Title: "Make, change and delete my workspaces", Class: authz.ClassSelf},
		},
	})
}

// sorted orders workspaces as the menu lists them: by position, then name.
func sorted(ws []Workspace) []Workspace {
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Position != ws[j].Position {
			return ws[i].Position < ws[j].Position
		}
		if ws[i].Name != ws[j].Name {
			return ws[i].Name < ws[j].Name
		}
		return ws[i].ID < ws[j].ID
	})
	return ws
}

// All reads user's workspaces in tenant, in the menu's order.
func All(ctx context.Context, store live.Store, tenant, user uint64) ([]Workspace, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := store.Watch(ctx, tenant, Projector, TypeOf(user)+".*")
	if err != nil {
		return nil, err
	}
	out := []Workspace{}
	for e := range entries {
		if e == nil {
			break
		}
		var w Workspace
		if !e.Deleted && json.Unmarshal(e.Value.Value, &w) == nil {
			out = append(out, w)
		}
	}
	return sorted(out), nil
}

// Normalized is a name as names are compared and /workspace matches them:
// letters and digits, lower case.
func Normalized(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type change struct {
	Workspace *Workspace      `json:"workspace,omitempty"`
	Name      string          `json:"name,omitempty"`
	Position  int             `json:"position,omitempty"`
	Layout    json.RawMessage `json:"layout,omitempty"`
	Context   json.RawMessage `json:"context,omitempty"`
}

// fold is the workspace.workspaces projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var w Workspace
	if f.State != nil {
		if err := json.Unmarshal(f.State, &w); err != nil {
			return nil, err
		}
	}
	var c change
	if err := json.Unmarshal(f.Event.Data, &c); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventAdded:
		if c.Workspace == nil {
			return f.State, nil
		}
		w = *c.Workspace
		w.CreatedAt = f.Event.Time
	case EventRenamed:
		w.Name = c.Name
	case EventMoved:
		w.Position = c.Position
	case EventUpdated:
		if c.Layout != nil {
			w.Layout = c.Layout
		}
		if c.Context != nil {
			w.Context = c.Context
		}
	case EventDeleted:
		return nil, nil
	default:
		return f.State, nil
	}
	w.Version++
	w.UpdatedAt = f.Event.Time
	return json.Marshal(w)
}

func problem(code, path, message, fix string) []live.Problem {
	return []live.Problem{{Code: code, Path: path, Message: message, Fix: fix}}
}

// checkName trims a name and refuses an empty, long or taken one: names
// are unique per person, compared by Normalized.
func checkName(name, self string, others []Workspace) (string, []live.Problem) {
	name = strings.TrimSpace(name)
	switch {
	case Normalized(name) == "":
		return "", problem("", "args.name", "a workspace needs a name with letters or digits", "")
	case utf8.RuneCountInString(name) > MaxName:
		return "", problem("", "args.name", fmt.Sprintf("a workspace's name is at most %d characters", MaxName), "")
	}
	for _, o := range others {
		if o.ID != self && Normalized(o.Name) == Normalized(name) {
			return "", problem("workspace_name_taken", "args.name", fmt.Sprintf("you already have a workspace called %q", o.Name), "choose another name")
		}
	}
	return name, nil
}

// NewID is a new workspace's id.
func NewID() string { return "w" + strconv.FormatUint(features.NewTSID(), 10) }

const describeFields = `{id, name, isDefault, position, layout, context, version, createdAt, updatedAt}`

// Contribute registers the projector, the workspace.* commands, the
// workspace.list query and the workspace.list tool; deps returns their
// dependencies once made. It runs before livetools.Contribute, which gives
// each command that is not internal its tool.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	// Who has a workspace open (forge 16b: the windows on one workspace) is
	// presence in ws_<userId>.<id>, which only that person may see
	presence.Owned("ws")
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: source}, Title: "Workspaces",
		Description: "Each person's workspaces (forge 16a), keyed ws_<userId>.<id>: " + describeFields,
		Area:        Area, Fold: fold,
	})
	others := func(d live.Decision) ([]Workspace, []live.Problem) {
		dd := deps()
		if dd.Store == nil {
			return nil, nil
		}
		all, err := All(d.Context(), dd.Store, d.Principal.TenantID, d.Principal.UserID)
		if err != nil {
			return nil, problem("", "", "your workspaces cannot be read: "+err.Error(), "")
		}
		return all, nil
	}
	register := func(c live.Command, decide func(d live.Decision, w Workspace, exists bool) ([]live.NewEvent, []live.Problem)) {
		c.Source = source
		c.Area, c.TypeFor, c.Projection = Area, typeFor, Projector
		if c.Action == "" {
			c.Action = "workspace.write"
		}
		c.Resource = func(p features.Principal, _ live.Target) string { return Resource(p) }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if d.Principal.Kind != features.PrincipalUser || d.Principal.UserID == 0 {
				return nil, problem("", "", "only people have workspaces", "")
			}
			var w Workspace
			exists := d.State != nil
			if exists {
				if err := json.Unmarshal(d.State, &w); err != nil {
					return nil, problem("", "", "the workspace cannot be read: "+err.Error(), "")
				}
			}
			return decide(d, w, exists)
		}
		live.Commands.Register(reg, c)
	}
	missing := func(d live.Decision) []live.Problem {
		return problem("workspace_not_found", "target", fmt.Sprintf("you have no workspace %q", d.Target.ID), "list your workspaces with workspace.list")
	}

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.makeDefault"}, Title: "Make the Default workspace", Internal: true,
		Description: "Makes the person's Default workspace when they have none; the first read of their workspaces sends it.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventAdded},
	}, func(d live.Decision, _ Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if exists {
			return nil, nil
		}
		if d.Target.ID != DefaultID {
			return nil, problem("", "target", "the Default workspace's id is "+DefaultID, "")
		}
		return []live.NewEvent{{Type: EventAdded, Data: change{Workspace: &Workspace{ID: DefaultID, Name: DefaultName, IsDefault: true}}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.create"}, Title: "Make a workspace",
		Description: "Makes a workspace of yours, after the others in the Workspaces menu. With from, it duplicates that workspace's open editors and active Volumes and Chronicle entries. It does not switch to it: in the chat, switch with workspace.switch.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"What the Workspaces menu calls it, unique among yours, at most 60 characters, e.g. Support"},
  "from":{"type":"string","description":"The id of a workspace of yours to duplicate"}},"required":["name"]}`),
		Examples:  []json.RawMessage{json.RawMessage(`{"name":"Support"}`)},
		Events:    []string{EventAdded},
		NewTarget: NewID,
	}, func(d live.Decision, _ Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if exists {
			return nil, nil // a create sent again
		}
		var a struct {
			Name string `json:"name"`
			From string `json:"from"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		all, problems := others(d)
		if problems != nil {
			return nil, problems
		}
		name, problems := checkName(a.Name, d.Target.ID, all)
		if problems != nil {
			return nil, problems
		}
		w := Workspace{ID: d.Target.ID, Name: name}
		for _, o := range all {
			w.Position = max(w.Position, o.Position+1)
		}
		if a.From != "" {
			raw, err := d.Read(Projector, live.Target{Area: Area, Type: d.Target.Type, ID: a.From})
			if err != nil {
				return nil, problem("", "args.from", "the workspace cannot be read: "+err.Error(), "")
			}
			if raw == nil {
				return nil, problem("workspace_not_found", "args.from", fmt.Sprintf("you have no workspace %q", a.From), "list your workspaces with workspace.list")
			}
			var from Workspace
			if err := json.Unmarshal(raw, &from); err != nil {
				return nil, problem("", "args.from", err.Error(), "")
			}
			w.Layout, w.Context = from.Layout, from.Context
		}
		return []live.NewEvent{{Type: EventAdded, Data: change{Workspace: &w}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.rename"}, Title: "Rename a workspace",
		Description: "Renames a workspace of yours (Default too).",
		Args:        json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Its new name, unique among yours, at most 60 characters"}},"required":["name"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"name":"Customer support"}`)},
		Events:      []string{EventRenamed},
	}, func(d live.Decision, w Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		all, problems := others(d)
		if problems != nil {
			return nil, problems
		}
		name, problems := checkName(a.Name, w.ID, all)
		if problems != nil {
			return nil, problems
		}
		if name == w.Name {
			return nil, nil
		}
		return []live.NewEvent{{Type: EventRenamed, Data: change{Name: name}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.move"}, Title: "Move a workspace",
		Description: "Moves a workspace of yours to a place in the Workspaces menu (0 is the top); the others keep their order around it.",
		Args:        json.RawMessage(`{"type":"object","properties":{"position":{"type":"integer","minimum":0,"description":"Its place in the menu, from 0 at the top"}},"required":["position"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"position":0}`)},
		Events:      []string{EventMoved},
	}, func(d live.Decision, w Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		var a struct {
			Position int `json:"position"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		all, problems := others(d)
		if problems != nil {
			return nil, problems
		}
		// Each other workspace is read through the decision, so the whole
		// renumbering commits only if none changed since
		order := []Workspace{}
		for _, o := range all {
			if o.ID == w.ID {
				continue
			}
			raw, err := d.Read(Projector, live.Target{Area: Area, Type: d.Target.Type, ID: o.ID})
			if err != nil {
				return nil, problem("", "", "your workspaces cannot be read: "+err.Error(), "")
			}
			if raw != nil && json.Unmarshal(raw, &o) == nil {
				order = append(order, o)
			}
		}
		order = sorted(order)
		at := min(a.Position, len(order))
		order = append(order[:at], append([]Workspace{w}, order[at:]...)...)
		var events []live.NewEvent
		for i, o := range order {
			if o.Position == i {
				continue
			}
			e := live.NewEvent{Type: EventMoved, Data: change{Position: i}}
			if o.ID != w.ID {
				e.Target = &live.Target{Area: Area, Type: d.Target.Type, ID: o.ID}
			}
			events = append(events, e)
		}
		return events, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.delete"}, Title: "Delete a workspace",
		Description: "Deletes a workspace of yours. Its chats stay in your other workspaces. Default cannot be deleted.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventDeleted},
	}, func(d live.Decision, w Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, nil // deleted already
		}
		if w.IsDefault {
			return nil, problem("default_workspace", "target", "the Default workspace cannot be deleted", "rename it instead")
		}
		return []live.NewEvent{{Type: EventDeleted, Data: change{}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "workspace.update"}, Title: "Save a workspace's layout or context", NoTool: true,
		Description: "Saves a workspace's open editors and layout (layout, forge 16b) or its active Volumes and Chronicle entries (context, 16c), as of the version the client read: a stale version is refused with stale_workspace, and the client merges and retries.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "version":{"type":"integer","description":"The workspace's version as the client read it"},
  "layout":{"type":"object","description":"The open editors and their layout"},
  "context":{"type":"object","description":"The active Volumes and Chronicle entries"}},"required":["version"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"version":3,"layout":{"tabs":[]}}`)},
		Events:   []string{EventUpdated},
	}, func(d live.Decision, w Workspace, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		var a struct {
			Version uint64          `json:"version"`
			Layout  json.RawMessage `json:"layout"`
			Context json.RawMessage `json:"context"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		if a.Version != w.Version {
			return nil, problem("stale_workspace", "args.version", fmt.Sprintf("the workspace changed since version %d: it is at %d", a.Version, w.Version), "read it again, merge and retry")
		}
		if a.Layout == nil && a.Context == nil {
			return nil, nil
		}
		if len(a.Layout) > MaxPart || len(a.Context) > MaxPart {
			return nil, problem("workspace_too_large", "args", fmt.Sprintf("a workspace's layout and context may hold at most %d KB each", MaxPart>>10), "close some editors")
		}
		if a.Context != nil {
			var c Context
			if err := json.Unmarshal(a.Context, &c); err != nil {
				return nil, problem("bad_context", "args.context", "a workspace's context is {kind: {id: checked}}, such as {\"volume\": {\"42\": false}}", "send each kind's items as an object of true or false")
			}
		}
		return []live.NewEvent{{Type: EventUpdated, Data: change{Layout: a.Layout, Context: a.Context}}}, nil
	})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "workspace.list", Source: source}, Title: "Your workspaces",
		Description: "Your workspaces in this tenant, in the Workspaces menu's order (forge 16a): {workspaces: [" + describeFields + "]}. Default is made the first time they are read.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "workspace.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run:         func(f live.Feed) error { return run(f, deps) },
	})

	contributeTools(reg, deps)
}

// EnsureDefault makes args' principal's Default workspace when they have
// none. The command id is fixed, and the commit expects no workspace
// there, so processes racing make one.
func EnsureDefault(args features.ServiceArgs, b *bus.Bus) *features.ServiceResponse {
	p := args.Principal
	res := b.Execute(bus.Request{ServiceArgs: args, Type: "workspace.makeDefault", Target: DefaultID, Args: json.RawMessage(`{}`),
		ID: fmt.Sprintf("ws-default-%d-%d", p.TenantID, p.UserID)})
	if res.Common.Code >= 300 {
		return &res.Common
	}
	return nil
}

// run is workspace.list: the person's workspaces, made Default first if
// needed, sent whole on every change.
func run(f live.Feed, deps func() Deps) error {
	args := f.Args()
	p := args.Principal
	if p.Kind != features.PrincipalUser || p.UserID == 0 {
		return fmt.Errorf("only people have workspaces")
	}
	d := deps()
	entries, err := f.Store().Watch(f.Context(), p.TenantID, Projector, TypeOf(p.UserID)+".*")
	if err != nil {
		return err
	}
	byID := map[string]Workspace{}
	started, waiting := false, false
	send := func() {
		out := make([]Workspace, 0, len(byID))
		for _, w := range byID {
			out = append(out, w)
		}
		raw, _ := json.Marshal(map[string]any{"workspaces": sorted(out)})
		if !started {
			started = true
			f.Snapshot(json.RawMessage(raw), live.Position{})
			return
		}
		f.Change(live.Delta{Op: "replace", Value: raw}, live.Position{})
	}
	for e := range entries {
		if e == nil {
			if _, ok := byID[DefaultID]; !ok && d.Bus != nil {
				if fail := EnsureDefault(args, d.Bus); fail == nil {
					waiting = true // the snapshot waits for Default
					continue
				}
			}
			send()
			continue
		}
		id := strings.TrimPrefix(e.Key, TypeOf(p.UserID)+".")
		var w Workspace
		if e.Deleted || json.Unmarshal(e.Value.Value, &w) != nil {
			delete(byID, id)
		} else {
			byID[id] = w
		}
		if started || (waiting && id == DefaultID) {
			send()
		}
	}
	return nil
}

// Name is the name of args' principal's workspace id, read from their own
// workspaces only; false when they have none by that id.
func Name(args features.ServiceArgs, store live.Store, id string) (string, bool) {
	w, ok := Get(args, store, id)
	if !ok || w.Name == "" {
		return "", false
	}
	return w.Name, true
}
