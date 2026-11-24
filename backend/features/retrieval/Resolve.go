package retrieval

import (
	"slices"
)

// Dials are one model call's retrieval settings (forge 15b): the Retrieval
// group of the Agent node's and Run mode's config. Each is optional: nil
// (or empty) leaves it to the preset, then to the tenant's chat default.
type Dials struct {
	// Preset is a chat.context.presets id; empty is Custom.
	Preset           string `json:"preset,omitempty"`
	LexicalSemantic  *int   `json:"lexicalSemantic,omitempty"`  // −5 … +5
	ChronicleLibrary *int   `json:"chronicleLibrary,omitempty"` // −5 … +5
	TopK             *int   `json:"topK,omitempty"`             // 1 … 20
	// Sources are library, chronicle and records; empty keeps the default's.
	Sources []string `json:"sources,omitempty"`
}

// Empty says no dial is set: the call searches as the tenant's chat does.
func (d Dials) Empty() bool {
	return d.Preset == "" && d.LexicalSemantic == nil && d.ChronicleLibrary == nil && d.TopK == nil && len(d.Sources) == 0
}

// Preset is a named position of the dials (the chat.context.presets
// section's items, as retrievalsettings.Preset).
type Preset struct {
	ID               string
	LexicalSemantic  int
	ChronicleLibrary int
	TopK             int
}

// Resolve works out a call's options (forge 15b): the preset's values,
// then the dials set on the node, which win over the preset's, then the
// tenant's chat default (chat.context.defaults.rag, as
// retrievalcontext.OptionsOf reads it) for anything left empty. A preset
// that is not found is ignored.
func Resolve(d Dials, presets []Preset, defaults Options) Options {
	o := defaults
	o.VolumeIDs = nil
	if i := slices.IndexFunc(presets, func(p Preset) bool { return p.ID == d.Preset }); d.Preset != "" && i >= 0 {
		p := presets[i]
		o.LexicalSemantic, o.ChronicleLibrary = p.LexicalSemantic, p.ChronicleLibrary
		if p.TopK > 0 {
			o.TopK = p.TopK
		}
	}
	if d.LexicalSemantic != nil {
		o.LexicalSemantic = clampInt(*d.LexicalSemantic, -5, 5)
	}
	if d.ChronicleLibrary != nil {
		o.ChronicleLibrary = clampInt(*d.ChronicleLibrary, -5, 5)
	}
	if d.TopK != nil && *d.TopK > 0 {
		o.TopK = clampInt(*d.TopK, 1, 50)
	}
	if len(d.Sources) > 0 {
		o.Sources = append([]string{"none"}, d.Sources...)
	}
	return o
}

// Narrow is o narrowed by a run's retrieval input (forge 15a, 15b): its
// Volumes replace o's, and its sources, when it names any, are intersected
// with o's. It never widens: a source o does not search stays off.
func (o Options) Narrow(pin Options) Options {
	o.VolumeIDs = pin.VolumeIDs
	if len(pin.Sources) == 0 {
		return o
	}
	// No sources searches every source that is not optional, which records is
	allowed := func(id string) bool {
		if len(o.Sources) == 0 {
			return id != "records"
		}
		return slices.Contains(o.Sources, id)
	}
	out := []string{"none"} // nothing left searches nothing
	for _, id := range pin.Sources {
		if id != "none" && allowed(id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	o.Sources = out
	return o
}

func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
