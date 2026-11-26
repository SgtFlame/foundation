// Package made holds the chat modes people make (forge 15h, Forge's Mode
// Builder; documentation/ChatCompletion.md › Changed in forge 15h). A made
// mode is two things, made together by chat.mode.create in one request:
//   - a published mode graph, made with bp.import from a template (by
//     default the editable built-in chatModeTemplate) or given whole, and
//     edited later like any graph, by bp.* or in the Cognition editor;
//   - its entry, a record of the live core on the def stream (target type
//     chatMode, projection chat.modes.made): label, icon, purpose, focus,
//     the graph, whose it is and whether it is shared with the tenant,
//     changed only by the chat.mode.* commands, each an op with its
//     inverse.
//
// The chat lists them after the chat.modes settings
// (conversationsettings.ReadModes): a person sees their own and those
// shared with the tenant. Entries are records, not settings, so a person's
// modes never freeze the settings' list at the user scope.
package made

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	conversationsettings "github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// TargetType is a made mode's target type, and Projector its projection.
const (
	TargetType = "chatMode"
	Projector  = "chat.modes.made"
	// TemplateID is the graph a made mode starts from when chat.mode.create
	// names none.
	TemplateID = "chatModeTemplate"
	source     = "conversation"
)

// Events of a made mode.
const (
	EventAdded   = "added"   // {mode}: made, or brought back by an undo
	EventUpdated = "updated" // {mode}: label, icon, purpose, focus or graph changed
	EventShared  = "shared"  // {shared}
	EventDeleted = "deleted" // {}
)

// Mode is a made mode's entry, as the projection keeps it.
type Mode struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	Icon     string              `json:"icon,omitempty"`
	Purpose  string              `json:"purpose"`
	GraphRef string              `json:"graphRef"`
	Focus    *conversation.Focus `json:"focus,omitempty"`
	Owner    uint64              `json:"owner,string"` // the user who made it
	Shared   bool                `json:"shared,omitempty"`
}

// Setting is the mode as the chat lists it, for p.
func (m Mode) Setting(p features.Principal) conversationsettings.Mode {
	return conversationsettings.Mode{ID: m.ID, Label: m.Label, Icon: m.Icon, GraphRef: m.GraphRef, Focus: m.Focus, Purpose: m.Purpose,
		Made: true, Mine: p.Kind == features.PrincipalUser && m.Owner == p.UserID, Shared: m.Shared}
}

// visibleTo reports whether p sees the mode: its maker, and everyone in
// the tenant once it is shared.
func (m Mode) visibleTo(p features.Principal) bool {
	return m.Shared || (p.Kind == features.PrincipalUser && m.Owner == p.UserID)
}

// App is an app with tabs, where a mode may be offered.
type App struct {
	ID, Title, Icon string
	Kinds           []string // its editor kinds, app:<appId>:<kind>
}

// Deps are what made modes need, made after the registry.
type Deps struct {
	Store  live.Store
	Bus    *bus.Bus
	Engine *engine.Engine
	Config config.Reader
	// Apps lists the tenant's apps with tabs (ui's editor kinds), for the
	// places a mode may be offered; nil offers only the built-in places.
	Apps func(args features.ServiceArgs) []App
}

// All reads every made mode of the tenant, by label.
func All(ctx context.Context, store live.Store, tenant uint64) ([]Mode, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := store.Watch(ctx, tenant, Projector, TargetType+".*")
	if err != nil {
		return nil, err
	}
	var out []Mode
	for e := range entries {
		if e == nil {
			break
		}
		var m Mode
		if !e.Deleted && json.Unmarshal(e.Value.Value, &m) == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Label < out[j].Label || (out[i].Label == out[j].Label && out[i].ID < out[j].ID)
	})
	return out, nil
}

// Source is conversationsettings.MadeModes over the projection.
type Source struct{ Deps func() Deps }

// Visible is the made modes args' principal sees.
func (s Source) Visible(args features.ServiceArgs) ([]conversationsettings.Mode, error) {
	d := s.Deps()
	if d.Store == nil {
		return nil, nil
	}
	all, err := All(args.Context(), d.Store, args.Principal.TenantID)
	if err != nil {
		return nil, err
	}
	var out []conversationsettings.Mode
	for _, m := range all {
		if m.visibleTo(args.Principal) {
			out = append(out, m.Setting(args.Principal))
		}
	}
	return out, nil
}

// fold is the chat.modes.made projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var m Mode
	if f.State != nil {
		if err := json.Unmarshal(f.State, &m); err != nil {
			return nil, err
		}
	}
	var c change
	if err := json.Unmarshal(f.Event.Data, &c); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventAdded, EventUpdated:
		if c.Mode != nil {
			shared := m.Shared
			m = *c.Mode
			if f.Event.Type == EventUpdated {
				m.Shared = shared
			}
		}
	case EventShared:
		m.Shared = c.Shared
	case EventDeleted:
		return nil, nil // a removed mode leaves nothing: undo adds it again
	default:
		return f.State, nil
	}
	return json.Marshal(m)
}

type change struct {
	Mode   *Mode `json:"mode,omitempty"`
	Shared bool  `json:"shared,omitempty"`
}

func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

// fields are the arguments chat.mode.add and chat.mode.update take.
type fields struct {
	Label    *string             `json:"label"`
	Icon     *string             `json:"icon"`
	Purpose  *string             `json:"purpose"`
	GraphRef *string             `json:"graphRef"`
	Focus    *conversation.Focus `json:"focus"`
	// Everywhere clears the focus in an update: offered on every tab
	Everywhere bool `json:"everywhere"`
}

// Normalized is a label as /mode matches it: letters and digits, lower
// case, so "🎧 Support answers" and "support-answers" are one.
func Normalized(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sharesResource is where sharing a mode with the tenant is checked: the
// tenant's chat.modes settings (15h question 28).
func sharesResource(p features.Principal) string {
	return authz.Tenant(p) + "/config:" + conversationsettings.ModesID
}

// check refuses a mode that cannot be offered: no label, a label another
// mode the principal sees has, an unknown focus kind or a graph that is
// not a published mode graph.
func check(d live.Decision, deps Deps, m Mode) []live.Problem {
	switch {
	case strings.TrimSpace(m.Label) == "" || Normalized(m.Label) == "":
		return problem("args.label", "a mode needs a label with letters or digits", "")
	case utf8.RuneCountInString(m.Label) > 40:
		return problem("args.label", "a mode's label is at most 40 characters", "")
	case utf8.RuneCountInString(m.Icon) > 8:
		return problem("args.icon", "a mode's icon is one emoji", "")
	case strings.TrimSpace(m.Purpose) == "":
		return problem("args.purpose", "a mode needs a purpose: what it is for, in a sentence, which the chat suggests it by", "")
	case utf8.RuneCountInString(m.Purpose) > 400:
		return problem("args.purpose", "a mode's purpose is at most 400 characters: keep it short and distinct", "")
	}
	if deps.Config != nil {
		modes, err := conversationsettings.ReadModes(deps.Config, d.ServiceArgs)
		if err != nil {
			return problem("", "the chat modes cannot be read: "+err.Error(), "")
		}
		for _, other := range modes.Modes {
			if other.ID != m.ID && Normalized(other.Label) == Normalized(m.Label) {
				return problem("args.label", fmt.Sprintf("there is already a mode called %q", other.Label), "choose another label")
			}
		}
	}
	if m.Focus != nil {
		known := knownKinds(d.ServiceArgs, deps)
		for _, k := range m.Focus.Kinds {
			if !slices.Contains(known, k) {
				return problem("args.focus.kinds", fmt.Sprintf("there is no place %q to offer a mode", k), "choose a place chat.mode.options lists")
			}
		}
		if len(m.Focus.Kinds) == 0 && len(m.Focus.Subjects) == 0 {
			return problem("args.focus", "a focus names where the mode is offered", "leave the focus out to offer it everywhere")
		}
	}
	g, ok := graphOf(d, deps, m.GraphRef)
	if !ok {
		return problem("args.graphRef", fmt.Sprintf("there is no published graph %q", m.GraphRef), "publish it first with bp.publish")
	}
	if g.Kind != cognition.KindMode {
		return problem("args.graphRef", fmt.Sprintf("%s is a %s graph, not a chat mode", g.Name, g.Kind), "")
	}
	return nil
}

// graphOf is the graph a mode runs: its published version (read through
// the decision, so one published in the same request counts), else a
// built-in.
func graphOf(d live.Decision, deps Deps, id string) (cognition.Graph, bool) {
	if !live.ValidToken(id) {
		return cognition.Graph{}, false
	}
	if d.Read != nil {
		if raw, err := d.Read(drafts.PublishedProjector, live.Target{Area: live.AreaDefinitions, Type: drafts.TargetType, ID: id}); err == nil && raw != nil {
			var g cognition.Graph
			if json.Unmarshal(raw, &g) == nil && g.Kind != "" {
				return g, true
			}
		}
	}
	if deps.Engine != nil {
		return deps.Engine.Graph(d.ServiceArgs, id)
	}
	return cognition.Graph{}, false
}

// knownKinds are the focus kinds a mode may name: every app's tab (app:*),
// one app's tabs (app:<appId>:*) and its editor kinds, and those the
// contributed modes are offered on (Forge's editors).
func knownKinds(args features.ServiceArgs, deps Deps) []string {
	kinds := []string{"app:*"}
	if deps.Apps != nil {
		for _, a := range deps.Apps(args) {
			kinds = append(kinds, "app:"+a.ID+":*")
			kinds = append(kinds, a.Kinds...)
		}
	}
	for _, p := range contributedPlaces() {
		if p.Focus != nil {
			kinds = append(kinds, p.Focus.Kinds...)
		}
	}
	return kinds
}

// contributedPlaces are where the contributed modes are offered, read
// from the chat.mode point when asked (Contribute may run before modes
// contributed later, such as Forge's builders).
var contributedPlaces = func() []Place { return nil }

func placesOf(reg *extensions.Registry) []Place {
	var out []Place
	for _, m := range conversation.Modes.All(reg) {
		if m.Focus != nil && len(m.Focus.Kinds) > 0 {
			out = append(out, Place{Label: "where " + strings.TrimSpace(m.Icon+" "+m.Label) + " is offered", Focus: &conversation.Focus{Kinds: m.Focus.Kinds, Subjects: m.Focus.Subjects}})
		}
	}
	return out
}

// Place is somewhere a mode may be offered, in words.
type Place struct {
	Label string              `json:"label"`
	Focus *conversation.Focus `json:"focus,omitempty"` // none: everywhere
}

func (f fields) apply(m *Mode) {
	if f.Label != nil {
		m.Label = strings.TrimSpace(*f.Label)
	}
	if f.Icon != nil {
		m.Icon = strings.TrimSpace(*f.Icon)
	}
	if f.Purpose != nil {
		m.Purpose = strings.TrimSpace(*f.Purpose)
	}
	if f.GraphRef != nil {
		m.GraphRef = *f.GraphRef
	}
	if f.Focus != nil {
		m.Focus = f.Focus
	}
	if f.Everywhere {
		m.Focus = nil
	}
}

// argsOf is a mode's fields as chat.mode.add or chat.mode.update take
// them, for an inverse.
func argsOf(m Mode) map[string]any {
	a := map[string]any{"label": m.Label, "icon": m.Icon, "purpose": m.Purpose, "graphRef": m.GraphRef}
	if m.Focus != nil {
		a["focus"] = m.Focus
	} else {
		a["everywhere"] = true
	}
	return a
}

// mayChange refuses a change by anyone but the mode's maker, or, for a
// shared mode, someone who may share modes with the tenant.
func mayChange(d live.Decision, m Mode) []live.Problem {
	p := d.Principal
	if p.Kind == features.PrincipalUser && m.Owner == p.UserID {
		return nil
	}
	if m.Shared && authz.Check(d.Context(), p, "config.write", sharesResource(p)).Allowed {
		return nil
	}
	return []live.Problem{{Code: "access_denied", Path: "target", Message: fmt.Sprintf("the mode %q is someone else's", m.Label)}}
}

const modeArgs = `"label":{"type":"string","description":"What the mode menu calls it, e.g. Support answers"},
  "icon":{"type":"string","description":"One emoji shown before the label, e.g. 🎧"},
  "purpose":{"type":"string","description":"What it is for, in a sentence: the chat suggests switching to it by this, so keep it short and distinct from the other modes'"},
  "graphRef":{"type":"string","description":"The published mode graph each round runs"},
  "focus":{"type":"object","description":"Where it is offered, from chat.mode.options' places; leave it out to offer it everywhere","properties":{"kinds":{"type":"array","items":{"type":"string"}},"subjects":{"type":"array","items":{"type":"string"}},"offerOnly":{"type":"boolean"}}}`

// Contribute registers the projector, the chat.mode.* commands, the
// chat.modes.made query, the template graph and the tools; deps returns
// their dependencies once made. It runs before livetools.Contribute, which
// gives each command its tool.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	contributedPlaces = func() []Place { return placesOf(reg) }
	var template cognition.Graph
	if err := json.Unmarshal(templateJSON, &template); err != nil {
		panic("made: the template graph: " + err.Error()) // embedded, so a test catches it
	}
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: TemplateID, Order: 150, Source: source}, Graph: template, Editable: true})

	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: source}, Title: "Made chat modes",
		Description: "Each chat mode a person made (forge 15h): {id, label, icon, purpose, graphRef, focus, owner, shared}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	read := func(d live.Decision) (Mode, bool, []live.Problem) {
		var m Mode
		if d.State == nil {
			return m, false, nil
		}
		if err := json.Unmarshal(d.State, &m); err != nil {
			return m, false, problem("", "the mode cannot be read: "+err.Error(), "")
		}
		return m, true, nil
	}
	register := func(id, title, description, args, example string, events []string, decide func(d live.Decision, m Mode, exists bool) ([]live.NewEvent, []live.Problem)) {
		cmd := live.Command{
			Descriptor: extensions.Descriptor{ID: id, Source: source}, Title: title, Description: description,
			Args: json.RawMessage(args), Examples: []json.RawMessage{json.RawMessage(example)}, Events: events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: Projector, Action: "conversation.write",
			Resource: func(p features.Principal, _ live.Target) string {
				return authz.Tenant(p) + "/" + authz.Resource("user", p.UserID) + "/conversations"
			},
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				if d.Principal.Kind != features.PrincipalUser {
					return nil, problem("", "only people make and change chat modes", "")
				}
				m, exists, problems := read(d)
				if problems != nil {
					return nil, problems
				}
				return decide(d, m, exists)
			},
		}
		if id == "chat.mode.add" {
			cmd.NewTarget = func() string { return NewID() }
		}
		live.Commands.Register(reg, cmd)
	}

	register("chat.mode.add", "Add a chat mode",
		"Adds a chat mode of yours to the mode menu, running a published mode graph (graphRef). chat.mode.create makes the graph and the mode together, which is usually what you want; use this for a graph you made yourself with bp.*. The mode is yours alone until shared with chat.mode.share.",
		`{"type":"object","properties":{`+modeArgs+`},"required":["label","purpose","graphRef"]}`,
		`{"label":"Support answers","icon":"🎧","purpose":"Answers customers' support questions briefly from the Support Volume.","graphRef":"mode-1"}`,
		[]string{EventAdded},
		func(d live.Decision, old Mode, exists bool) ([]live.NewEvent, []live.Problem) {
			if exists {
				return nil, problem("target", fmt.Sprintf("there is already a mode %q", d.Target.ID), "change it with chat.mode.update")
			}
			if !live.ValidToken(d.Target.ID) {
				return nil, problem("target", fmt.Sprintf("%q is not a mode id", d.Target.ID), "leave the target out to get a new one")
			}
			var f fields
			if err := json.Unmarshal(d.Args, &f); err != nil {
				return nil, problem("args", err.Error(), "")
			}
			m := Mode{ID: d.Target.ID, Owner: d.Principal.UserID}
			f.apply(&m)
			deps := deps()
			if problems := check(d, deps, m); problems != nil {
				return nil, problems
			}
			d.Undo("", live.OpOf("chat.mode.delete", m.ID, map[string]any{}))
			return []live.NewEvent{{Type: EventAdded, Data: change{Mode: &m}}}, nil
		})

	register("chat.mode.update", "Change a chat mode",
		"Changes a chat mode you made: its label, icon, purpose, where it is offered (focus, or everywhere: true) or the graph it runs. Change what it does by editing its graph (bp.*, forge.read graph:<graphRef>).",
		`{"type":"object","properties":{`+modeArgs+`,"everywhere":{"type":"boolean","description":"Offer it everywhere, clearing its focus"}}}`,
		`{"label":"Support replies","icon":"💬"}`,
		[]string{EventUpdated},
		func(d live.Decision, old Mode, exists bool) ([]live.NewEvent, []live.Problem) {
			if !exists {
				return nil, problem("target", fmt.Sprintf("there is no mode %q", d.Target.ID), "list the modes with chat.mode.options")
			}
			if problems := mayChange(d, old); problems != nil {
				return nil, problems
			}
			var f fields
			if err := json.Unmarshal(d.Args, &f); err != nil {
				return nil, problem("args", err.Error(), "")
			}
			m := old
			f.apply(&m)
			if same(m, old) {
				d.Undo("")
				return nil, nil
			}
			if problems := check(d, deps(), m); problems != nil {
				return nil, problems
			}
			d.Undo("", live.OpOf("chat.mode.update", m.ID, argsOf(old)))
			return []live.NewEvent{{Type: EventUpdated, Data: change{Mode: &m}}}, nil
		})

	register("chat.mode.share", "Share a chat mode",
		"Shares a chat mode you made with everyone in the tenant (shared: true), or makes it yours alone again (false). Needs the right to change the tenant's chat settings (config.write).",
		`{"type":"object","properties":{"shared":{"type":"boolean","description":"Offer it to everyone in the tenant"}},"required":["shared"]}`,
		`{"shared":true}`,
		[]string{EventShared},
		func(d live.Decision, old Mode, exists bool) ([]live.NewEvent, []live.Problem) {
			if !exists {
				return nil, problem("target", fmt.Sprintf("there is no mode %q", d.Target.ID), "list the modes with chat.mode.options")
			}
			if problems := mayChange(d, old); problems != nil {
				return nil, problems
			}
			var a struct{ Shared bool }
			if err := json.Unmarshal(d.Args, &a); err != nil {
				return nil, problem("args", err.Error(), "")
			}
			if a.Shared == old.Shared {
				d.Undo("")
				return nil, nil
			}
			if p := d.Principal; !authz.Check(d.Context(), p, "config.write", sharesResource(p)).Allowed {
				return nil, []live.Problem{{Code: "access_denied", Path: "args.shared", Message: "sharing a mode with the tenant needs the right to change its chat settings", Fix: "ask a tenant administrator to share it"}}
			}
			d.Undo("", live.OpOf("chat.mode.share", old.ID, map[string]any{"shared": old.Shared}))
			return []live.NewEvent{{Type: EventShared, Data: change{Shared: a.Shared}}}, nil
		})

	register("chat.mode.delete", "Remove a chat mode",
		"Removes a chat mode you made from the mode menu. Its graph stays, editable in the Cognition editor (bp.delete removes it); undo brings the mode back.",
		`{"type":"object","properties":{}}`, `{}`,
		[]string{EventDeleted},
		func(d live.Decision, old Mode, exists bool) ([]live.NewEvent, []live.Problem) {
			if !exists {
				d.Undo("")
				return nil, nil
			}
			if problems := mayChange(d, old); problems != nil {
				return nil, problems
			}
			inverse := []live.Op{live.OpOf("chat.mode.add", old.ID, argsOf(old))}
			if old.Shared {
				inverse = append(inverse, live.OpOf("chat.mode.share", old.ID, map[string]any{"shared": true}))
			}
			d.Undo("", inverse...)
			return []live.NewEvent{{Type: EventDeleted, Data: change{}}}, nil
		})

	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "chat.modes.made", Source: source}, Title: "Made chat modes",
		Description: "The chat modes people made that you see (forge 15h): your own and those shared with the tenant, each {id, label, icon, purpose, graphRef, focus, owner, shared}. The chat's mode menu lists them after the settings' modes.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "conversation.read",
		Run: func(f live.Feed) error {
			p := f.Args().Principal
			entries, err := f.Store().Watch(f.Context(), p.TenantID, Projector, TargetType+".*")
			if err != nil {
				return err
			}
			byID := map[string]Mode{}
			started := false
			send := func(pos live.Position) {
				out := []Mode{}
				for _, m := range byID {
					if m.visibleTo(p) {
						out = append(out, m)
					}
				}
				sort.Slice(out, func(i, j int) bool {
					return out[i].Label < out[j].Label || (out[i].Label == out[j].Label && out[i].ID < out[j].ID)
				})
				raw, _ := json.Marshal(map[string]any{"modes": out})
				if !started {
					started = true
					f.Snapshot(json.RawMessage(raw), pos)
					return
				}
				f.Change(live.Delta{Op: "replace", Value: raw}, pos)
			}
			for e := range entries {
				if e == nil {
					send(live.Position{})
					continue
				}
				id := strings.TrimPrefix(e.Key, TargetType+".")
				var m Mode
				if e.Deleted || json.Unmarshal(e.Value.Value, &m) != nil {
					delete(byID, id)
				} else {
					byID[id] = m
				}
				if started {
					send(live.Position{})
				}
			}
			return nil
		},
	})

	contributeTools(reg, deps)
}

func same(a, b Mode) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// NewID is a new made mode's id, also its graph's: mode-<tsid>.
func NewID() string { return "mode-" + strconv.FormatUint(features.NewTSID(), 10) }

// AppsOf is the apps with tabs ui's editor kinds list, for Deps.Apps.
func AppsOf(apps []ui.App, kinds []ui.EditorKind) []App {
	var out []App
	for _, a := range apps {
		app := App{ID: a.ID, Title: a.Title, Icon: a.Icon}
		for _, k := range kinds {
			if k.App == a.ID {
				app.Kinds = append(app.Kinds, k.ID)
			}
		}
		if len(app.Kinds) > 0 {
			out = append(out, app)
		}
	}
	return out
}
