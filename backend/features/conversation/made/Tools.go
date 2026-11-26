package made

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	conversationsettings "github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

//go:embed template.json
var templateJSON []byte

// The template's nodes chat.mode.create sets.
const (
	NodeGather       = "gather"       // chat.gather: the context the mode looks at
	NodeInstructions = "instructions" // text.template: its instructions
	NodeToolList     = "toolList"     // data.expression: the tools it may use, a list
)

// ClientTools are the chat's browser tools a mode may offer beside the
// server's (common's chat.clientTool contributions).
var ClientTools = []string{"chat.askChoice", "chat.askForm", "workbench.open", "workbench.getState"}

// contextWords say in plain words what each gathering context provider
// gives a mode; another provider is described by its title.
var contextWords = map[string]string{
	"workbench": "what the person has open in the workbench (the editors, their subjects and selections)",
	"activity":  "what the person just did by hand, since their last message",
	"rag":       "passages from the person's Volumes and Chronicles, searched for each message and cited",
}

// CreateArgs are chat.mode.create's.
type CreateArgs struct {
	Label        string              `json:"label"`
	Icon         string              `json:"icon"`
	Purpose      string              `json:"purpose"`
	Focus        *conversation.Focus `json:"focus"`
	Instructions string              `json:"instructions"`
	Context      []string            `json:"context"`
	Tools        []string            `json:"tools"`
	From         string              `json:"from"`
	Graph        *cognition.Graph    `json:"graph"`
}

// escape keeps text a template prints as it is: {{ starts an action.
func escape(text string) string {
	return strings.ReplaceAll(text, "{{", `{{"{{"}}`)
}

// Build is the mode graph chat.mode.create would make from base: its name
// and description from the label and purpose, and, when given, the
// instructions, context and tools in the template's nodes. A node it must
// set and base lacks is an error naming it.
func Build(base cognition.Graph, id string, a CreateArgs) (cognition.Graph, error) {
	g := base
	raw, _ := json.Marshal(base)
	_ = json.Unmarshal(raw, &g) // a copy: the nodes' configs are shared otherwise
	if g.Kind != cognition.KindMode {
		return g, fmt.Errorf("%s is a %s graph, not a chat mode", base.Name, base.Kind)
	}
	g.ID, g.Name, g.Description, g.Version = id, strings.TrimSpace(a.Label), strings.TrimSpace(a.Purpose), 0
	set := func(node string, config map[string]any) error {
		i := slices.IndexFunc(g.Nodes, func(n cognition.Node) bool { return n.ID == node })
		if i < 0 {
			return fmt.Errorf("the graph %s has no node %q to set: give the whole graph, or start from %s", base.ID, node, TemplateID)
		}
		var c map[string]any
		_ = json.Unmarshal(g.Nodes[i].Config, &c)
		if c == nil {
			c = map[string]any{}
		}
		for k, v := range config {
			c[k] = v
		}
		g.Nodes[i].Config, _ = json.Marshal(c)
		return nil
	}
	if a.Instructions != "" {
		text := "{{.system}}\n\n# Mode: " + escape(strings.TrimSpace(a.Label)) + "\n" + escape(strings.TrimSpace(a.Instructions))
		if err := set(NodeInstructions, map[string]any{"template": text}); err != nil {
			return g, err
		}
	}
	if a.Context != nil {
		if err := set(NodeGather, map[string]any{"providers": a.Context}); err != nil {
			return g, err
		}
	}
	if a.Tools != nil {
		list, _ := json.Marshal(a.Tools)
		if err := set(NodeToolList, map[string]any{"expr": string(list)}); err != nil {
			return g, err
		}
	}
	if a.Context != nil || a.Tools != nil {
		settings := slices.Clone(a.Context)
		if a.Context == nil {
			settings = cognition.ModeSpecOf(base).Settings
			settings = slices.DeleteFunc(slices.Clone(settings), func(s string) bool { return s == "tools" || s == "debug" })
		}
		if a.Tools == nil || len(a.Tools) > 0 {
			settings = append(settings, "tools")
		}
		settings = append(settings, "debug")
		g.Spec, _ = json.Marshal(map[string]any{"settings": settings})
	}
	return g, nil
}

// gathering are the context providers a Gather context node can run.
func gathering(reg *extensions.Registry) []conversation.ContextProvider {
	var out []conversation.ContextProvider
	for _, p := range conversation.ContextProviders.All(reg) {
		if p.Gather != nil {
			out = append(out, p)
		}
	}
	return out
}

// offerable are the tool names a mode of args' principal may offer.
func offerable(reg *extensions.Registry, args features.ServiceArgs) map[string]tools.Tool {
	out := map[string]tools.Tool{}
	for _, t := range tools.Available(reg, args) {
		out[t.ID] = t
	}
	for _, id := range ClientTools {
		if _, ok := out[id]; !ok {
			out[id] = tools.Tool{Descriptor: extensions.Descriptor{ID: id}, Title: id, Effect: tools.EffectUI, Location: tools.LocationClient}
		}
	}
	return out
}

func contributeTools(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "chat.mode.options", Source: source}, Title: "Chat mode options", Effect: tools.EffectRead,
		Description: "What a new chat mode may be made of (forge 15h): the modes you see now (yours marked mine; a label must differ from theirs), the context a mode may look at, the places it may be offered (focus), the tools you may give it grouped by app (each \"name: title\", marked (asks) when it asks the person each time; give group to see one group's tools with their descriptions), the graphs it may start from, and whether you may share a mode with the tenant. Call it before chat.mode.create or chat.mode.update.",
		Input:       json.RawMessage(`{"type":"object","properties":{"group":{"type":"string","description":"One tool group (an app or prefix, such as mail or kb): only its tools, each with its description"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Group string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			d := deps()
			args := call.ServiceArgs
			var modes []map[string]any
			if d.Config != nil {
				all, err := conversationsettings.ReadModes(d.Config, args)
				if err != nil {
					return tools.Result{}, fmt.Errorf("the chat modes cannot be read")
				}
				for _, m := range all.Modes {
					modes = append(modes, map[string]any{"id": m.ID, "label": m.Label, "icon": m.Icon, "purpose": m.Purpose, "graphRef": m.GraphRef,
						"focus": m.Focus, "made": m.Made, "mine": m.Mine, "shared": m.Shared})
				}
			}
			context := []map[string]any{}
			for _, p := range gathering(reg) {
				words := contextWords[p.ID]
				if words == "" {
					words = p.Title
				}
				context = append(context, map[string]any{"id": p.ID, "title": p.Title, "gives": words})
			}
			places := []Place{{Label: "everywhere"}, {Label: "on any app's tab", Focus: &conversation.Focus{Kinds: []string{"app:*"}}}}
			if d.Apps != nil {
				for _, a := range d.Apps(args) {
					places = append(places, Place{Label: "on " + a.Title + "'s tabs", Focus: &conversation.Focus{Kinds: []string{"app:" + a.ID + ":*"}}})
				}
			}
			places = append(places, contributedPlaces()...)
			// Names and titles only, so the options fit a tool result (the
			// tools section's maxResultKB) however many tools there are; a
			// group's descriptions on request
			groups := map[string][]any{}
			for id, t := range offerable(reg, args) {
				group, _, _ := strings.Cut(id, ".")
				if a.Group != "" && group != a.Group {
					continue
				}
				asks := t.Effect == tools.EffectWrite || t.Effect == tools.EffectExternal
				if a.Group != "" {
					groups[group] = append(groups[group], map[string]any{"name": id, "title": t.Title, "asks": asks, "description": t.Description})
					continue
				}
				item := id + ": " + t.Title // "mail.label: Label a thread (asks)"
				if asks {
					item += " (asks)"
				}
				groups[group] = append(groups[group], item)
			}
			if a.Group != "" && len(groups) == 0 {
				return tools.Result{}, fmt.Errorf("there is no tool group %q you may give a mode", a.Group)
			}
			names := make([]string, 0, len(groups))
			for g := range groups {
				names = append(names, g)
			}
			sort.Strings(names)
			toolGroups := []map[string]any{}
			for _, g := range names {
				list := groups[g]
				sort.Slice(list, func(i, j int) bool { return fmt.Sprint(list[i]) < fmt.Sprint(list[j]) })
				toolGroups = append(toolGroups, map[string]any{"group": g, "tools": list})
			}
			if a.Group != "" {
				return tools.Result{Content: map[string]any{"toolGroups": toolGroups}, Summary: fmt.Sprintf("%d tools in %s", len(toolGroups[0]["tools"].([]any)), a.Group)}, nil
			}
			templates := []map[string]any{}
			if d.Engine != nil {
				for _, g := range d.Engine.Graphs() {
					if g.Kind == cognition.KindMode {
						if pub, ok := d.Engine.Graph(args, g.ID); ok {
							g = pub
						}
						templates = append(templates, map[string]any{"graph": g.ID, "name": g.Name, "description": g.Description, "default": g.ID == TemplateID})
					}
				}
			}
			p := call.Principal
			content := map[string]any{"modes": modes, "context": context, "places": places, "toolGroups": toolGroups, "templates": templates,
				"mayShare":   authz.Check(call.Context(), p, "config.write", sharesResource(p)).Allowed,
				"writeTools": "a write tool still asks the person each time the mode uses it; no mode can turn that off"}
			return tools.Result{Content: content, Summary: fmt.Sprintf("%d modes, %d places, %d tool groups", len(modes), len(places), len(toolGroups))}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "chat.mode.create", Source: source}, Title: "Make a chat mode", Effect: tools.EffectWrite,
		Description: "Makes a chat mode of yours, in one change the person approves and can undo: a published mode graph and its place in the mode menu. By default the graph is a copy of " + TemplateID + " with your instructions (plain text after the chat's system prompt), the context it looks at (context: ids from chat.mode.options) and the tools it may use (tools: names from chat.mode.options; [] offers none); from names another mode graph to copy (it must have the nodes gather, instructions and toolList to set those), and graph gives the whole graph (kind mode: nodes, edges, spec) for anything the template cannot do. The mode is yours alone until shared (chat.mode.share). Its graph can be changed later with bp.* or in the Cognition editor.",
		Input: json.RawMessage(`{"type":"object","properties":{
  "label":{"type":"string","description":"What the mode menu calls it, e.g. Support answers"},
  "icon":{"type":"string","description":"One emoji, e.g. 🎧"},
  "purpose":{"type":"string","description":"What it is for, in a sentence: the chat suggests switching to it by this, so keep it short and distinct from the other modes'"},
  "focus":{"type":"object","description":"Where it is offered: a place's focus from chat.mode.options; leave it out to offer it everywhere","properties":{"kinds":{"type":"array","items":{"type":"string"}},"subjects":{"type":"array","items":{"type":"string"}},"offerOnly":{"type":"boolean"}}},
  "instructions":{"type":"string","description":"How it answers, in plain words: its role, its steps, its tone and length, what it must never do"},
  "context":{"type":"array","items":{"type":"string"},"description":"The context it looks at for each message: ids from chat.mode.options (workbench, activity, rag)"},
  "tools":{"type":"array","items":{"type":"string"},"description":"The tools it may use, by name, from chat.mode.options; [] for none"},
  "from":{"type":"string","description":"A mode graph to start from, from chat.mode.options' templates; ` + TemplateID + ` when left out"},
  "graph":{"type":"object","description":"The whole mode graph, in place of from (bp.* queries' shape: {kind: mode, nodes, edges, spec}), for a mode the template cannot express"}},
 "required":["label","purpose"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[CreateArgs](call)
			if err != nil {
				return tools.Result{}, err
			}
			d := deps()
			if d.Bus == nil || d.Engine == nil {
				return tools.Result{}, fmt.Errorf("chat modes cannot be made here")
			}
			args := call.ServiceArgs
			may := offerable(reg, args)
			for _, name := range a.Tools {
				if _, ok := may[name]; !ok {
					return tools.Result{}, fmt.Errorf("the tool %q is not one you may give a mode: choose from chat.mode.options' toolGroups", name)
				}
			}
			for _, id := range a.Context {
				if !slices.ContainsFunc(gathering(reg), func(p conversation.ContextProvider) bool { return p.ID == id }) {
					return tools.Result{}, fmt.Errorf("there is no context %q: choose from chat.mode.options' context", id)
				}
			}
			var base cognition.Graph
			switch {
			case a.Graph != nil:
				base = *a.Graph
				if base.Name == "" {
					base.Name = a.Label
				}
			default:
				from := a.From
				if from == "" {
					from = TemplateID
				}
				g, ok := d.Engine.Graph(args, from)
				if !ok {
					return tools.Result{}, fmt.Errorf("there is no mode graph %q to start from", from)
				}
				base = g
			}
			id := NewID()
			g, err := Build(base, id, a)
			if err != nil {
				return tools.Result{}, err
			}
			var errs []string
			for _, p := range d.Engine.Resolve(g).Problems {
				if p.Severity == cognition.SeverityError {
					where := p.NodeID
					if p.Pin != "" {
						where += "." + p.Pin
					}
					errs = append(errs, strings.TrimPrefix(where+": "+p.Message, ": "))
				}
			}
			if len(errs) > 0 {
				return tools.Result{Content: map[string]any{"status": "refused", "problems": errs}}, fmt.Errorf("the mode's graph has problems, so nothing was made: %s", strings.Join(errs, "; "))
			}
			entry := map[string]any{"label": a.Label, "icon": a.Icon, "purpose": a.Purpose, "graphRef": id}
			if a.Focus != nil {
				entry["focus"] = a.Focus
			}
			ops := []live.Op{
				live.OpOf("bp.import", id, map[string]any{"graph": g}),
				live.OpOf("bp.publish", id, map[string]any{"rev": 1}),
				live.OpOf("chat.mode.add", id, entry),
			}
			t := d.Bus.Transact(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("tool-%d", features.NewTSID()), RequestID: livetools.RequestOf(call),
				Actor: livetools.ActorOf(call)}, ops, false)
			if t.Common.Code >= 300 {
				if len(t.Problems) == 0 {
					return tools.Result{}, fmt.Errorf("%s", t.Common.Message)
				}
				p := t.Problems[0]
				msg := "the mode was not made: " + p.Message
				if p.Fix != "" {
					msg += "; fix: " + p.Fix
				}
				return tools.Result{Content: map[string]any{"status": "refused", "problems": t.Problems}}, fmt.Errorf("%s", msg)
			}
			title := strings.TrimSpace(a.Icon + " " + a.Label)
			return tools.Result{
				Content: map[string]any{"status": "made", "mode": id, "graph": id, "label": a.Label, "icon": a.Icon, "request": t.RequestID, "seq": t.Seq},
				Summary: fmt.Sprintf("Made %s, now in your mode menu", title), // no ids: the person reads it (15h)
			}, nil
		},
	})
}
