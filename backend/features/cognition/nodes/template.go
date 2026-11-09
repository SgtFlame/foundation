package cognitionnodes

import (
	"encoding/json"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/tmpl"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Node type IDs of the template nodes (forge 06i).
const (
	Template = "text.template"
	Setting  = "data.setting"
)

// templateConfig is a Template node's config.
type templateConfig struct {
	Template string `json:"template"`
	// Inputs declare pins the template does not name at its top, such as
	// those it reads only through a variable; their types type the pins.
	Inputs []cognition.Pin `json:"inputs,omitempty"`
}

// TemplatePins are the input pins of a template: its declared inputs, then
// one optional pin of any type per field it reads (tmpl.Fields).
func TemplatePins(text string, declared []cognition.Pin) ([]cognition.Pin, error) {
	fields, err := tmpl.Fields(text)
	if err != nil {
		return nil, err
	}
	pins := make([]cognition.Pin, 0, len(declared)+len(fields))
	for _, p := range declared {
		p.Exec, p.Optional = false, true
		if p.Type == "" {
			p.Type = cognition.Any
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.Description == "" {
			p.Description = "The template's ." + p.ID
		}
		pins = append(pins, p)
	}
	for _, f := range fields {
		if slices.ContainsFunc(pins, func(p cognition.Pin) bool { return p.ID == f }) {
			continue
		}
		pins = append(pins, cognition.Pin{ID: f, Name: f, Type: cognition.Any, Optional: true, Description: "The template's ." + f})
	}
	return pins, nil
}

// TemplateData is a node's inputs as a template's data, with nil for each
// field it reads that is not connected, so {{if .x}} works.
func TemplateData(pins []cognition.Pin, in cognition.Values) map[string]any {
	data := make(map[string]any, len(pins))
	for _, p := range pins {
		data[p.ID] = normalize(in[p.ID])
	}
	return data
}

func contributeTemplates(reg *extensions.Registry) {
	node(reg, 475, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: Template}, Title: "Template", Category: "Data",
		Description: "Renders a Go text/template: each .name it reads at the top becomes an input pin, and it may call join, upper, lower, trim, indent, clip, oneLine, replace, split, contains, hasPrefix, hasSuffix, default, json, list, dict, tail, add, sub, now and setting \"section\" \"path\". Build prompts and tool descriptions with it.",
		Example:     "Template \"You are {{.persona}}.{{if .tools}} You may call: {{join \", \" .tools}}.{{end}} Steps: {{setting \"builder\" \"maxSteps\"}}\"",
		Examples: []json.RawMessage{json.RawMessage(`{"type":"text.template","config":{"template":"Hello, {{.name}}!{{range .items}}\n- {{.}}{{end}}"}}`)},
		Tags:     []string{"prompt", "text", "go template"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"template":{"type":"string","title":"Template","format":"multiline","description":"A Go text/template; the dot is the node's inputs"},
			"inputs":{"type":"array","title":"Inputs","description":"Pins it reads through variables, with their types","items":{"type":"object","properties":{"id":{"type":"string","title":"ID"},"name":{"type":"string","title":"Name"},"type":{"type":"string","title":"Type"},"description":{"type":"string","title":"Description"}}}}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c templateConfig
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			in, err := TemplatePins(c.Template, c.Inputs)
			return cognition.Pins{In: in, Out: []cognition.Pin{cognition.DataPin("text", "Text", "string", "The rendered text")}}, err
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c templateConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			pins, err := TemplatePins(c.Template, c.Inputs)
			if err != nil {
				return nil, "", err
			}
			out, err := x.Render(c.Template, TemplateData(pins, in))
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"text": out}, "", nil
		},
	})
	node(reg, 476, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: Setting}, Title: "Setting", Category: "Data",
		Description: "A configuration setting as the run's user sees it, after the app, tenant and user scopes: a section and a dotted path in it, or the whole section",
		Example:     "Setting chat systemPrompt, the instructions a chat mode's prompt starts with",
		Examples:    []json.RawMessage{json.RawMessage(`{"type":"data.setting","config":{"section":"chat","path":"systemPrompt"}}`)},
		Volatile:    true,
		Config: json.RawMessage(`{"type":"object","properties":{
			"section":{"type":"string","title":"Section","description":"The configuration section's id, e.g. chat"},
			"path":{"type":"string","title":"Path","description":"A dotted path in it, e.g. systemPrompt; empty gives the whole section"},
			"type":{"type":"string","title":"Type","description":"The value's type, for its pin"}},"required":["section"]}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c struct{ Type string }
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			if c.Type == "" {
				c.Type = cognition.Any
			}
			return cognition.Pins{Out: []cognition.Pin{cognition.DataPin("value", "Value", c.Type, "The setting's value; nothing when it is not set")}}, nil
		},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			var c struct{ Section, Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			v, err := x.Setting(c.Section, c.Path)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": v}, "", nil
		},
	})
}
