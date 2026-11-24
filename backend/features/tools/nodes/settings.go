package toolsnodes

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	retrievalcontext "github.com/SgtFlame/foundation/backend/features/retrieval/context"
	"github.com/SgtFlame/foundation/backend/features/retrieval/settings"
)

// ModelSettings are a model node's explicit settings (forge 15b): Ask
// model's, the Agent node's, Stream reply's and Run mode's. Each is a
// config field and a matching optional pin; the pin wins, then the config,
// then the level fills what is left unset.
type ModelSettings struct {
	Model           string   `json:"model,omitempty"` // an ai.models id
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	ThinkingBudget  *int     `json:"thinkingBudget,omitempty"`
}

// ModelProps are the model settings' config properties, to put in a node's
// config schema.
const ModelProps = `"model":{"type":"string","title":"Model","ref":"ai.models#models","description":"A chat model from the catalog, in place of the level's; empty keeps the level's"},
"temperature":{"type":"number","title":"Temperature","min":0,"max":2,"step":0.1,"description":"0 is the most predictable; empty keeps the level's"},
"maxOutputTokens":{"type":"integer","title":"Max output tokens","min":1,"max":65536,"description":"Empty keeps the level's"},
"thinkingBudget":{"type":"integer","title":"Thinking budget","min":-1,"max":32768,"description":"-1 lets the model decide, 0 turns thinking off where the model allows; empty keeps the level's"}`

// ModelPins are the model settings' optional input pins.
func ModelPins() []cognition.Pin {
	return []cognition.Pin{
		{ID: "model", Name: "Model", Type: "string", Optional: true, Description: "An ai.models id, in place of its config's"},
		{ID: "temperature", Name: "Temperature", Type: "number", Optional: true, Description: "0 to 2, in place of its config's"},
		{ID: "maxOutputTokens", Name: "Max output tokens", Type: "number", Optional: true, Description: "In place of its config's"},
		{ID: "thinkingBudget", Name: "Thinking budget", Type: "number", Optional: true, Description: "-1 lets the model decide, 0 turns thinking off; in place of its config's"},
	}
}

// ModelSettingsOf are a node's model settings: its config's, with each
// joined pin in place of its field.
func ModelSettingsOf(x *cognition.Exec, in cognition.Values) (ModelSettings, error) {
	var m ModelSettings
	if err := x.Config(&m); err != nil {
		return m, err
	}
	joined := func(id string) bool { v, ok := in[id]; return ok && v != nil }
	if joined("model") {
		s, err := cognition.Get[string](in, "model")
		if err != nil {
			return m, err
		}
		if s != "" {
			m.Model = s
		}
	}
	if joined("temperature") {
		t, err := cognition.Get[float64](in, "temperature")
		if err != nil {
			return m, err
		}
		m.Temperature = &t
	}
	if joined("maxOutputTokens") {
		n, err := cognition.Get[float64](in, "maxOutputTokens")
		if err != nil {
			return m, err
		}
		m.MaxOutputTokens = int(n)
	}
	if joined("thinkingBudget") {
		n, err := cognition.Get[float64](in, "thinkingBudget")
		if err != nil {
			return m, err
		}
		b := int(n)
		m.ThinkingBudget = &b
	}
	switch {
	case m.Temperature != nil && (*m.Temperature < 0 || *m.Temperature > 2):
		return m, fmt.Errorf("the temperature goes from 0 to 2, not %v", *m.Temperature)
	case m.MaxOutputTokens < 0:
		return m, fmt.Errorf("max output tokens cannot be negative")
	case m.ThinkingBudget != nil && *m.ThinkingBudget < -1:
		return m, fmt.Errorf("the thinking budget is -1, 0 or more")
	}
	return m, nil
}

// Apply sets what m names on a model call; the level fills the rest.
func (m ModelSettings) Apply(a *chatservice.CompleteArgs) {
	if m.Model != "" {
		a.ModelRef = m.Model
	}
	if m.Temperature != nil {
		t := float32(*m.Temperature)
		a.Temperature = &t
	}
	if m.MaxOutputTokens > 0 {
		n := m.MaxOutputTokens
		a.MaxTokens = &n
	}
	if m.ThinkingBudget != nil {
		b := *m.ThinkingBudget
		a.ThinkingBudget = &b
	}
}

// RetrievalProps is the Retrieval group's config property (forge 15b), to
// put in a node's config schema: a preset, the dials as sliders, Top-K and
// the sources. An empty group searches as the tenant's chat does.
const RetrievalProps = `"rag":{"type":"object","title":"Retrieval","description":"How kb.search searches during this call: a preset, or the dials. Empty searches as the tenant's chat does by default","properties":{
"preset":{"type":"string","title":"Preset","ref":"chat.context.presets#presets","description":"The preset the dials start from; empty is Custom"},
"lexicalSemantic":{"type":"integer","title":"Lexical ↔ Semantic","min":-5,"max":5,"widget":"slider","description":"−5 favours exact keywords (names, codes); +5 favours meaning. Empty keeps the preset's"},
"chronicleLibrary":{"type":"integer","title":"Chronicles ↔ Library","min":-5,"max":5,"widget":"slider","description":"−5 favours Chronicle memory; +5 favours Volumes. Empty keeps the preset's"},
"topK":{"type":"integer","title":"Top-K","min":1,"max":20,"widget":"slider","description":"How many passages a search gives at most. Empty keeps the preset's"},
"sources":{"type":"object","title":"Sources","description":"What it searches; empty keeps the tenant's chat default","properties":{
"library":{"type":"boolean","title":"Volumes"},
"chronicle":{"type":"boolean","title":"Chronicles"},
"records":{"type":"boolean","title":"Records","description":"Applications' records, by their searchable fields"}}}}}`

// DialsConfig is the Retrieval group as a node's config holds it.
type DialsConfig struct {
	Preset           string          `json:"preset,omitempty"`
	LexicalSemantic  *int            `json:"lexicalSemantic,omitempty"`
	ChronicleLibrary *int            `json:"chronicleLibrary,omitempty"`
	TopK             *int            `json:"topK,omitempty"`
	Sources          map[string]bool `json:"sources,omitempty"`
}

// Dials are the group as retrieval.Resolve takes it. Sources named follow
// RAG Search: Volumes and Chronicles unless switched off, records only
// when switched on.
func (c DialsConfig) Dials() retrieval.Dials {
	d := retrieval.Dials{Preset: c.Preset, LexicalSemantic: c.LexicalSemantic, ChronicleLibrary: c.ChronicleLibrary, TopK: c.TopK}
	if len(c.Sources) > 0 {
		for _, id := range []string{"library", "chronicle"} {
			if on, set := c.Sources[id]; !set || on {
				d.Sources = append(d.Sources, id)
			}
		}
		if c.Sources["records"] {
			d.Sources = append(d.Sources, "records")
		}
		if len(d.Sources) == 0 {
			d.Sources = []string{"none"}
		}
	}
	return d
}

// RetrievalOf works out a model call's retrieval options (forge 15b): the
// node's Retrieval group resolved over the presets and the tenant's chat
// default, then narrowed by the run's retrieval input (pin), which brings
// the Volumes and never widens the sources. ok is false when neither the
// group nor the pin says anything, so the call searches as before 15b.
func RetrievalOf(x *cognition.Exec, cfg config.Reader, group DialsConfig, pin any) (retrieval.Options, bool, error) {
	narrow, pinned, err := retrieval.ParseOptions(pin)
	if err != nil {
		return retrieval.Options{}, false, err
	}
	dials := group.Dials()
	if dials.Empty() && !pinned {
		return retrieval.Options{}, false, nil
	}
	presets, defaults := presetsAndDefaults(x, cfg)
	o := retrieval.Resolve(dials, presets, defaults)
	if pinned {
		// The pin's own dials (from before 15b) still count when the node sets none
		if dials.Empty() {
			if narrow.LexicalSemantic != 0 {
				o.LexicalSemantic = narrow.LexicalSemantic
			}
			if narrow.ChronicleLibrary != 0 {
				o.ChronicleLibrary = narrow.ChronicleLibrary
			}
			if narrow.TopK != 0 {
				o.TopK = narrow.TopK
			}
		}
		o = o.Narrow(narrow)
	}
	return o, true, nil
}

// RetrievalDefaults are the tenant's chat retrieval default
// (chat.context.defaults.rag) as options: what an empty Retrieval group
// searches with.
func RetrievalDefaults(x *cognition.Exec, cfg config.Reader) retrieval.Options {
	_, defaults := presetsAndDefaults(x, cfg)
	return retrieval.Resolve(retrieval.Dials{}, nil, defaults)
}

func presetsAndDefaults(x *cognition.Exec, cfg config.Reader) ([]retrieval.Preset, retrieval.Options) {
	var presets []retrieval.Preset
	defaults := retrieval.Options{}
	if cfg == nil {
		return nil, defaults
	}
	if p, err := config.Read[retrievalsettings.Presets](cfg, x.ServiceArgs, retrievalsettings.PresetsID); err == nil {
		for _, item := range p.Presets {
			presets = append(presets, retrieval.Preset{ID: item.ID, LexicalSemantic: item.LexicalSemantic, ChronicleLibrary: item.ChronicleLibrary, TopK: item.TopK})
		}
	}
	if raw, err := cfg.Resolved(x.ServiceArgs, conversation.DefaultsSection(retrievalcontext.ID)); err == nil {
		defaults = retrievalcontext.OptionsOf(raw)
	}
	return presets, defaults
}

// RAGSettings are options as RAG Search's settings (the rag provider's), so
// a headless host gathers with them as a round does.
func RAGSettings(o retrieval.Options) json.RawMessage {
	sources := map[string]bool{"library": false, "chronicle": false, "records": false}
	if len(o.Sources) == 0 {
		sources["library"], sources["chronicle"] = true, true
	}
	for _, id := range o.Sources {
		if _, ok := sources[id]; ok {
			sources[id] = true
		}
	}
	b, _ := json.Marshal(map[string]any{"enabled": true, "lexicalSemantic": o.LexicalSemantic, "chronicleLibrary": o.ChronicleLibrary,
		"topK": o.TopK, "sources": sources})
	return b
}
