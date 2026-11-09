// Package cognitionnodes contributes the Cognition Engine's own node types,
// data types, conversions and the core graph kinds: Flow (Branch, Return,
// On call, Sequence, For each, Loop, Call graph), Data (Get and Set
// variable, Reroute) and Notes (comment frames). Features contribute their
// own: Chat's events in conversation/nodes, Chronicle's steps in
// chronicle/nodes, the blueprint kinds in live/nodes and ui/nodes.
package cognitionnodes

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

const source = "cognition"

// Node type IDs other packages refer to.
const (
	CallGraph   = "flow.callGraph"
	GetVariable = "data.getVariable"
	SetVariable = "data.setVariable"
	Reroute     = "flow.reroute"
	Comment     = "note.comment"
)

// Contribute registers the core node and data types, conversions and kinds.
func Contribute(reg *extensions.Registry) {
	for i, t := range []cognition.DataType{
		{Descriptor: extensions.Descriptor{ID: "string"}, Color: "#e91e8c", Description: "Text"},
		{Descriptor: extensions.Descriptor{ID: "number"}, Color: "#4caf50", Description: "A number"},
		{Descriptor: extensions.Descriptor{ID: "bool"}, Color: "#c62828", Description: "True or false"},
		{Descriptor: extensions.Descriptor{ID: "json"}, Color: "#ff9800", Description: "Any JSON value"},
		{Descriptor: extensions.Descriptor{ID: cognition.Any}, Color: "#9e9e9e", Description: "Any type: takes the type of what it is joined to"},
	} {
		t.Order, t.Source = i, source
		cognition.Types.Register(reg, t)
	}
	contributeConversions(reg)
	contributeKinds(reg)
	contributeFlow(reg)
	contributeData(reg)
	contributeDataNodes(reg)
	contributeTemplates(reg)
}

func contributeConversions(reg *extensions.Registry) {
	same := func(v any) (any, error) { return v, nil }
	text := func(v any) (any, error) {
		switch t := v.(type) {
		case string:
			return t, nil
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), nil
		case bool:
			return strconv.FormatBool(t), nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(b), nil
	}
	for i, c := range []cognition.Conversion{
		{From: "number", To: "string", Convert: text},
		{From: "bool", To: "string", Convert: text},
		{From: "json", To: "string", Convert: text},
		{From: "string", To: "json", Convert: same},
		{From: "number", To: "json", Convert: same},
		{From: "bool", To: "json", Convert: same},
	} {
		c.Descriptor = extensions.Descriptor{ID: c.From + "→" + c.To, Order: i, Source: source}
		cognition.Conversions.Register(reg, c)
	}
}

func contributeKinds(reg *extensions.Registry) {
	for i, k := range []cognition.Kind{
		{Descriptor: extensions.Descriptor{ID: cognition.KindMode}, Title: "Chat mode", Events: []string{"event.userMessage"},
			Description: "A chat mode, offered in the composer by the chat.modes settings: runs a whole chat round from On user message, gathering its context, building its prompt and tools with templates, streaming the reply with Stream reply, then naming the conversation and learning from the round",
			Returns: "nothing: the round's events come from its Stream reply, Say, Set title and Emit followed nodes",
			Spec:    json.RawMessage(modeSpecSchema)},
		{Descriptor: extensions.Descriptor{ID: cognition.KindAgent}, Title: "Agent", Events: []string{cognition.EventCall},
			Description: "A persona with its prompts, tools and level, called from other graphs with Call graph", Returns: "its declared outputs"},
		{Descriptor: extensions.Descriptor{ID: cognition.KindFunction}, Title: "Function", Events: []string{cognition.EventCall},
			Description: "A sub-graph with typed inputs and outputs, called from other graphs with Call graph", Returns: "its declared outputs"},
		{Descriptor: extensions.Descriptor{ID: cognition.KindSummarizer}, Title: "Summarizer",
			Description: "A Chronicle custom type's summarizing mode", Returns: "the summaries"},
	} {
		k.Order, k.Source = i*10, source
		cognition.Kinds.Register(reg, k)
	}
}

// modeSpecSchema is a mode graph's Spec (cognition.ModeSpec).
const modeSpecSchema = `{
  "type": "object",
  "properties": {
    "settings": {"type": "array", "items": {"type": "string"}, "description": "The context settings the mode uses, by chat.contextProvider id (chronicle, rag, workbench, tools, debug): the composer's ⚙ shows only these; empty shows them all"}
  }
}`

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Order, t.Source = order, source
	cognition.Nodes.Register(reg, t)
}

func contributeFlow(reg *extensions.Registry) {
	node(reg, 100, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.branch"},
		Title:       "Branch",
		Category:    "Flow",
		Description: "Continues by True or False, as the condition is",
		Example:     "Branch on Same port type? to connect the ports or else toast that they are incompatible",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("condition", "Condition", "bool", "Which way to continue")},
			Out: []cognition.Pin{cognition.ExecPin("true", "True"), cognition.ExecPin("false", "False")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			ok, err := cognition.Get[bool](in, "condition")
			if err != nil {
				return nil, "", err
			}
			if ok {
				return nil, "true", nil
			}
			return nil, "false", nil
		},
	})

	node(reg, 110, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.sequence"},
		Title:       "Sequence",
		Category:    "Flow",
		Description: "Runs each of its chains in turn, Then 1 to its end, then Then 2, and so on",
		Example:     "A Sequence whose Then 1 sets the state and whose Then 2 shows a toast",
		Config:      json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","title":"Chains","min":2,"max":16}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c struct{ Count int }
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			c.Count = min(max(c.Count, 2), 16)
			out := make([]cognition.Pin, c.Count)
			for i := range out {
				out[i] = cognition.ExecPin(fmt.Sprintf("then%d", i+1), fmt.Sprintf("Then %d", i+1))
			}
			return cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, "")}, Out: out}, nil
		},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			var c struct{ Count int }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			for i := range min(max(c.Count, 2), 16) {
				if err := x.Flow(fmt.Sprintf("then%d", i+1), ""); err != nil {
					return nil, "", err
				}
			}
			return nil, "", nil
		},
	})

	node(reg, 120, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.forEach"},
		Title:       "For each",
		Category:    "Flow",
		Description: "Runs its Body once for each item of a list, then continues by Completed",
		Example:     "For each edge the user deleted, send hello.disconnect with the item as the edge's id",
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), cognition.DataPin("list", "List", cognition.Any, "The items, a JSON array")},
			Out: []cognition.Pin{
				cognition.ExecPin("body", "Body"), cognition.ExecPin("completed", "Completed"),
				cognition.DataPin("item", "Item", cognition.Any, "The current item, inside the body"),
				cognition.DataPin("index", "Index", "number", "The current item's index, from 0"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			list, err := cognition.Get[[]any](in, "list")
			if err != nil {
				return nil, "", err
			}
			if len(list) > x.MaxIterations {
				return nil, "", fmt.Errorf("the list has %d items, more than the %d iterations a loop may run", len(list), x.MaxIterations)
			}
			for i, item := range list {
				x.Set(cognition.Values{"item": item, "index": i})
				if err := x.Flow("body", fmt.Sprintf("forEach:%s[%d]", x.Node.ID, i)); err != nil {
					return nil, "", err
				}
			}
			return nil, "completed", nil
		},
	})

	node(reg, 130, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.loop"},
		Title:       "Loop",
		Category:    "Flow",
		Description: "Runs its Body while its condition is true, at most Max times, then continues by Completed. The condition is evaluated again before each iteration.",
		Example:     "Loop while Index < 3 to retry a call three times",
		Config:      json.RawMessage(`{"type":"object","properties":{"max":{"type":"integer","title":"Max iterations","min":1,"max":1000000}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "condition", Name: "While", Type: "bool", Optional: true, Description: "Whether to run the body again; always when left unconnected"}},
			Out: []cognition.Pin{
				cognition.ExecPin("body", "Body"), cognition.ExecPin("completed", "Completed"),
				cognition.DataPin("index", "Index", "number", "The iteration, from 0"),
			},
		},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			var c struct{ Max int }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if c.Max <= 0 {
				c.Max = 100
			}
			for i := 0; i < c.Max; i++ {
				if i >= x.MaxIterations {
					return nil, "", fmt.Errorf("the loop ran more than the %d iterations a loop may run", x.MaxIterations)
				}
				frame := fmt.Sprintf("loop:%s[%d]", x.Node.ID, i)
				x.Set(cognition.Values{"index": i})
				cond, err := x.Pull("condition", frame)
				if err != nil {
					return nil, "", err
				}
				if b, ok := cond.(bool); cond != nil && (!ok || !b) {
					break
				}
				if err := x.Flow("body", frame); err != nil {
					return nil, "", err
				}
			}
			return nil, "completed", nil
		},
	})

	node(reg, 140, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: cognition.EventCall},
		Title:       "On call",
		Category:    "Events",
		Description: "Starts a function, an agent or an action when it is called; its outputs are the graph's declared inputs",
		Example:     "On call with an input Name, feeding a Template string that greets them",
		Event:       true,
		Config:      json.RawMessage(pinsSchema("Inputs")),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			inputs, err := cognition.DeclaredPins(config)
			if err != nil {
				return cognition.Pins{}, err
			}
			return cognition.Pins{Out: append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, inputs...)}, nil
		},
	})

	node(reg, 150, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: CallGraph},
		Title:       "Call graph",
		Category:    "Flow",
		Description: "Runs another graph from its On call event with these inputs, and gives its outputs. Its pins follow the called graph's signature as it was when the node was set.",
		Example:     "Call graph Format greeting with Name, then Set state greeting to its Text",
		Config: json.RawMessage(`{"type":"object","properties":{"graph":{"type":"string","title":"Graph","description":"The id of the graph to call"},
			"inputs":{"type":"array","title":"Inputs","readOnly":true},"outputs":{"type":"array","title":"Outputs","readOnly":true}}}`),
		Effects: true,
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c callConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			in := append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, c.Inputs...)
			for i := 1; i < len(in); i++ {
				in[i].Exec = false
			}
			out := append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, c.Outputs...)
			for i := 1; i < len(out); i++ {
				out[i].Exec, out[i].Optional = false, false
			}
			return cognition.Pins{In: in, Out: out}, nil
		},
		Normalize: func(n cognition.Normalizing) (json.RawMessage, error) {
			var c callConfig
			if len(n.Config) > 0 {
				if err := json.Unmarshal(n.Config, &c); err != nil {
					return nil, err
				}
			}
			c.Inputs, c.Outputs = nil, nil
			if c.Graph != "" {
				g, ok := n.Graph(c.Graph)
				if !ok {
					return nil, fmt.Errorf("there is no graph %q to call", c.Graph)
				}
				c.Inputs, c.Outputs = n.Signature(g)
			}
			return json.Marshal(c)
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c callConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if c.Graph == "" {
				return nil, "", fmt.Errorf("Call graph names no graph")
			}
			out, err := x.Call(c.Graph, in)
			return out, cognition.Then, err
		},
	})

	node(reg, 900, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: "flow.return"},
		Title:       "Return",
		Category:    "Flow",
		Description: "Ends the run with its inputs as the run's outputs",
		Example:     "Return with an output items, the context items a chat mode gathered",
		Config:      json.RawMessage(pinsSchema("Outputs")),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			outputs, err := cognition.DeclaredPins(config)
			if err != nil {
				return cognition.Pins{}, err
			}
			for i := range outputs {
				outputs[i].Optional = true
			}
			return cognition.Pins{In: append([]cognition.Pin{cognition.ExecPin(cognition.Then, "")}, outputs...)}, nil
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			x.Return(in)
			return nil, "", nil
		},
	})
}

type callConfig struct {
	Graph   string          `json:"graph"`
	Inputs  []cognition.Pin `json:"inputs,omitempty"`
	Outputs []cognition.Pin `json:"outputs,omitempty"`
}

type variableConfig struct {
	Variable string `json:"variable"`
	Type     string `json:"type"`
}

func variablePin(config json.RawMessage, id, name, description string) (cognition.Pin, error) {
	var c variableConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return cognition.Pin{}, err
		}
	}
	if c.Type == "" {
		c.Type = cognition.Any
	}
	return cognition.DataPin(id, name, c.Type, description), nil
}

func contributeData(reg *extensions.Registry) {
	varSchema := json.RawMessage(`{"type":"object","properties":{"variable":{"type":"string","title":"Variable","description":"The variable's id"},"type":{"type":"string","title":"Type"}}}`)
	node(reg, 200, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: GetVariable},
		Title:       "Get variable",
		Category:    "Data",
		Description: "The current value of one of the graph's variables",
		Example:     "Get variable attempts, compared with 3 to stop retrying",
		Config:      varSchema,
		Volatile:    true,
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			p, err := variablePin(config, "value", "Value", "The variable's value")
			return cognition.Pins{Out: []cognition.Pin{p}}, err
		},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			var c variableConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": x.Vars[c.Variable]}, "", nil
		},
	})
	node(reg, 210, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: SetVariable},
		Title:       "Set variable",
		Category:    "Data",
		Description: "Sets one of the graph's variables for the rest of the run",
		Example:     "Set variable attempts to Get variable attempts + 1 inside a Loop",
		Config:      varSchema,
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			in, err := variablePin(config, "value", "Value", "The variable's new value")
			out, _ := variablePin(config, "value", "Value", "The value set")
			return cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), in}, Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), out}}, err
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c variableConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			x.Vars[c.Variable] = in["value"]
			return cognition.Values{"value": in["value"]}, cognition.Then, nil
		},
	})
	node(reg, 220, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: Reroute},
		Title:       "Reroute",
		Category:    "Data",
		Description: "Passes a value on unchanged, to route a long wire around other nodes",
		Example:     "A Reroute between Get state count and a node far below it",
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("in", "", cognition.Any, "The value")},
			Out: []cognition.Pin{cognition.DataPin("out", "", cognition.Any, "The same value")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			return cognition.Values{"out": in["in"]}, "", nil
		},
	})
	node(reg, 230, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: Comment},
		Title:       "Comment",
		Category:    "Notes",
		Description: "A frame with a title and colour drawn behind nodes, to group and explain them; it never runs",
		Example:     "A Comment titled Validate the input around the Branch and its Toast",
		Config: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","title":"Title"},"color":{"type":"string","title":"Colour"},
			"width":{"type":"number","title":"Width"},"height":{"type":"number","title":"Height"},"collapsed":{"type":"boolean","title":"Collapsed"}}}`),
	})
}

func pinsSchema(title string) string {
	return `{
  "type": "object",
  "properties": {
    "outputs": {"type": "array", "title": "` + title + `", "items": {"type": "object", "properties": {
      "id": {"type": "string", "title": "ID"},
      "name": {"type": "string", "title": "Name"},
      "type": {"type": "string", "title": "Type"},
      "description": {"type": "string", "title": "Description"}
    }}}
  }
}`
}
