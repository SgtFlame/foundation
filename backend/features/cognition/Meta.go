package cognition

import (
	"encoding/json"
	"slices"
	"strings"
)

// ExamplesOf is a node type's examples as JSON, {type, config}: its own,
// or one made from its config schema's defaults (forge 06d question 4).
func ExamplesOf(t NodeType) []json.RawMessage {
	if len(t.Examples) > 0 {
		return t.Examples
	}
	ex := map[string]any{"type": t.ID}
	if config := ConfigDefaults(t.Config); len(config) > 0 {
		ex["config"] = config
	}
	raw, _ := json.Marshal(ex)
	return []json.RawMessage{raw}
}

// ConfigDefaults is a config schema's top-level defaults.
func ConfigDefaults(schema json.RawMessage) map[string]any {
	var s struct {
		Properties map[string]struct{ Default any }
	}
	_ = json.Unmarshal(schema, &s)
	out := map[string]any{}
	for k, p := range s.Properties {
		if p.Default != nil {
			out[k] = p.Default
		}
	}
	return out
}

// ConfigRequired is what a config schema requires.
func ConfigRequired(schema json.RawMessage) []string {
	var s struct{ Required []string }
	_ = json.Unmarshal(schema, &s)
	return s.Required
}

// TagsOf is a node type's tags: its own, its category, its source, its
// kinds, and event or effects when it starts runs or has side effects.
func TagsOf(t NodeType) []string {
	tags := slices.Clone(t.Tags)
	add := func(s string) {
		s = strings.ToLower(s)
		if s != "" && !slices.Contains(tags, s) {
			tags = append(tags, s)
		}
	}
	add(t.Category)
	add(t.Source)
	for _, k := range t.Kinds {
		add(k)
	}
	if t.Event {
		add("event")
	}
	if t.Effects {
		add("effects")
	}
	return tags
}

// ConfigExample is a node type's example as JSON: a node of type typ with
// config, itself JSON.
func ConfigExample(typ, config string) []json.RawMessage {
	return []json.RawMessage{json.RawMessage(`{"type":"` + typ + `","config":` + config + `}`)}
}
