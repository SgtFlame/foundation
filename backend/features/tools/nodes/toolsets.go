package toolsnodes

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// selectConfig is a Tools node's config.
type selectConfig struct {
	Include []string `json:"include"` // names, or prefixes ending in "."; empty takes every tool
	Exclude []string `json:"exclude"`
	// NoAsking leaves out the tools that ask first (writes), for a run
	// that cannot ask.
	NoAsking bool `json:"noAsking"`
}

// describeConfig is a Describe tool node's config.
type describeConfig struct {
	Tool        string `json:"tool"`
	Title       string `json:"title"`
	Description string `json:"description"` // a template
}

// graphToolConfig is a Graph tool node's config.
type graphToolConfig struct {
	Name           string `json:"name"`
	Title          string `json:"title"`
	Description    string `json:"description"` // a template
	Input          string `json:"input"`       // a template giving the arguments' JSON Schema, an object
	Graph          string `json:"graph"`
	Effect         string `json:"effect"`
	Asks           *bool  `json:"asks"`
	Summary        string `json:"summary"` // a template over the graph's outputs
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

var setPin = cognition.Pin{ID: "tools", Name: "Tools", Type: TypeToolSet, Optional: true, Description: "The tool set to add to"}

func outSet(description string) cognition.Pin {
	return cognition.DataPin("tools", "Tools", TypeToolSet, description)
}

// templatePins are the input pins of several templates, after first.
func templatePins(first []cognition.Pin, templates ...string) ([]cognition.Pin, error) {
	pins := slices.Clone(first)
	for _, t := range templates {
		more, err := cognitionnodes.TemplatePins(t, nil)
		if err != nil {
			return nil, err
		}
		for _, p := range more {
			if !slices.ContainsFunc(pins, func(q cognition.Pin) bool { return q.ID == p.ID }) {
				pins = append(pins, p)
			}
		}
	}
	return pins, nil
}

func contributeToolSets(reg *extensions.Registry, deps func() Deps) {
	node := func(order int, t cognition.NodeType) {
		t.Order, t.Source, t.Category = order, "tools", "Tools"
		cognition.Nodes.Register(reg, t)
	}
	node(310, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: SelectID},
		Title:       "Tools",
		Description: "Selects tools into a tool set by name or prefix (forge.): from the set joined to From, else from the server's tools the Tools settings leave on and, in a chat round, the browser's. Enabled false gives an empty set.",
		Example:     "Tools including forge., workbench. and chat., with Enabled from the conversation's tools setting, into Stream reply",
		Examples:    []json.RawMessage{json.RawMessage(`{"type":"tools.select","config":{"include":["kb.","chronicle."],"exclude":["kb.edit"]}}`)},
		Tags:        []string{"tools", "function calling"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"include":{"type":"array","title":"Include","items":{"type":"string"},"description":"Tool names, or prefixes ending in a dot (forge.); empty takes every tool"},
			"exclude":{"type":"array","title":"Exclude","items":{"type":"string"},"description":"Tool names or prefixes left out"},
			"noAsking":{"type":"boolean","title":"Leave out writes","description":"Leave out the tools that ask the user first, for a run that cannot ask"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				{ID: "from", Name: "From", Type: TypeToolSet, Optional: true, Description: "The set to select from; the server's and browser's tools when not joined"},
				{ID: "enabled", Name: "Enabled", Type: "bool", Optional: true, Description: "False gives no tools, e.g. the conversation's tools setting"},
			},
			Out: []cognition.Pin{outSet("The tools selected")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c selectConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if on, ok := in["enabled"].(bool); ok && !on {
				return cognition.Values{"tools": []tools.Spec{}}, "", nil
			}
			var from []tools.Spec
			if _, joined := in["from"]; joined {
				var err error
				if from, err = cognition.Get[[]tools.Spec](in, "from"); err != nil {
					return nil, "", err
				}
			} else {
				d := deps()
				server, client := tools.Tools.All(d.Registry), []tools.Tool(nil)
				if h, ok := x.Host.(Toolbox); ok {
					server, client = h.ServerTools(), h.ClientTools()
				}
				for _, t := range tools.Enabled(server, client, Settings(x, d)) {
					from = append(from, tools.SpecOf(t))
				}
			}
			out := []tools.Spec{}
			for _, s := range from {
				switch {
				case len(c.Include) > 0 && !tools.Matches(s.Name, c.Include):
				case tools.Matches(s.Name, c.Exclude):
				case c.NoAsking && (s.Asks != nil && *s.Asks || s.Asks == nil && s.Effect.Asks()):
				default:
					out = append(out, s)
				}
			}
			return cognition.Values{"tools": out}, "", nil
		},
	})
	node(320, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: DescribeID},
		Title:       "Describe tool",
		Description: "Gives one tool of a set a new description for the model, rendered from a Go template whose fields become input pins, and optionally a new title",
		Example:     "Describe tool forge.apply as \"Change {{.app}}'s drafts in small batches\"",
		Examples:    []json.RawMessage{json.RawMessage(`{"type":"tools.describe","config":{"tool":"kb.search","description":"Search the {{.team}} team's Volumes"}}`)},
		Tags:        []string{"tools", "prompt", "go template"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"tool":{"type":"string","title":"Tool","description":"The tool's name"},
			"title":{"type":"string","title":"Title","description":"Its card's title; empty keeps it"},
			"description":{"type":"string","title":"Description","format":"multiline","description":"A Go template for the model's description; empty keeps it"}},"required":["tool"]}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c describeConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			in := setPin
			in.Optional, in.Description = false, "The tool set holding the tool"
			pins, err := templatePins([]cognition.Pin{in}, c.Description)
			return cognition.Pins{In: pins, Out: []cognition.Pin{outSet("The set with the tool re-described")}}, err
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c describeConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			set, err := cognition.Get[[]tools.Spec](in, "tools")
			if err != nil {
				return nil, "", err
			}
			pins, err := templatePins(nil, c.Description)
			if err != nil {
				return nil, "", err
			}
			description := ""
			if c.Description != "" {
				if description, err = x.Render(c.Description, cognitionnodes.TemplateData(pins, in)); err != nil {
					return nil, "", err
				}
			}
			out := slices.Clone(set)
			for i := range out {
				if out[i].Name != c.Tool {
					continue
				}
				if description != "" {
					out[i].Description = description
				}
				if c.Title != "" {
					out[i].Title = c.Title
				}
			}
			return cognition.Values{"tools": out}, "", nil
		},
	})
	node(330, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: GraphToolID},
		Title:       "Graph tool",
		Description: "Adds a tool a function graph runs: the model sees its name, its description and its arguments' JSON Schema, all Go templates whose fields become input pins; a call runs the graph from On call with the arguments as inputs, and its outputs are the result",
		Example:     "Graph tool forge.build running the builder agent graph with appId, request and plan",
		Examples: []json.RawMessage{json.RawMessage(`{"type":"tools.graph","config":{"name":"team.lookup","description":"Looks up a teammate by name","graph":"lookupTeammate",` +
			`"input":"{\"type\":\"object\",\"properties\":{\"name\":{\"type\":\"string\"}},\"required\":[\"name\"]}","effect":"read"}}`)},
		Tags: []string{"tools", "function calling", "go template"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"name":{"type":"string","title":"Name","description":"The tool's dotted name, e.g. team.lookup"},
			"title":{"type":"string","title":"Title","description":"Its card's title"},
			"description":{"type":"string","title":"Description","format":"multiline","description":"A Go template: what it does and when to use it, for the model"},
			"input":{"type":"string","title":"Arguments","format":"multiline","description":"A Go template giving the arguments' JSON Schema, an object"},
			"graph":{"type":"string","title":"Graph","description":"The function or agent graph a call runs"},
			"effect":{"type":"enum","title":"Effect","enum":["none","read","write","external"],"description":"What a call does; write and external ask the user first unless Asks is off"},
			"asks":{"type":"boolean","title":"Asks","description":"Whether a call asks the user first; unset follows the effect"},
			"summary":{"type":"string","title":"Summary","description":"A Go template over the graph's outputs: the call card's line"},
			"timeoutSeconds":{"type":"integer","title":"Timeout","unit":"seconds","min":1,"max":7200,"description":"How long a call may run; empty keeps the Tools settings'"}},"required":["name","graph"]}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c graphToolConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			pins, err := templatePins([]cognition.Pin{setPin}, c.Description, c.Input)
			return cognition.Pins{In: pins, Out: []cognition.Pin{outSet("The set with the graph tool added")}}, err
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c graphToolConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if c.Name == "" || c.Graph == "" {
				return nil, "", fmt.Errorf("a graph tool needs a name and a graph")
			}
			set, err := cognition.Get[[]tools.Spec](in, "tools")
			if err != nil {
				return nil, "", err
			}
			pins, err := templatePins(nil, c.Description, c.Input)
			if err != nil {
				return nil, "", err
			}
			data := cognitionnodes.TemplateData(pins, in)
			description, err := x.Render(c.Description, data)
			if err != nil {
				return nil, "", err
			}
			spec := tools.Spec{Name: c.Name, Title: c.Title, Description: description, Effect: tools.Effect(c.Effect), Source: tools.SourceGraph,
				Graph: c.Graph, Asks: c.Asks, Summary: c.Summary, TimeoutSeconds: c.TimeoutSeconds}
			if c.Input != "" {
				input, err := x.Render(c.Input, data)
				if err != nil {
					return nil, "", err
				}
				var schema map[string]any
				if err := json.Unmarshal([]byte(input), &schema); err != nil {
					return nil, "", fmt.Errorf("the arguments of %s are not a JSON Schema object: %v", c.Name, err)
				}
				spec.Input = json.RawMessage(input)
			}
			out := slices.DeleteFunc(slices.Clone(set), func(s tools.Spec) bool { return s.Name == c.Name })
			return cognition.Values{"tools": append(out, spec)}, "", nil
		},
	})
}
