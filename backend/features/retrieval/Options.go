package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Options say how a tool's search runs (forge 15a): set on a call's
// context by whoever runs the tools, never by the model. A chat round sets
// its conversation's RAG Search settings; the Agent node sets its retrieval
// input, which an agent run fills from the agent's record. kb.search reads
// them; without them it searches as it always has (Volumes, dials at 0).
type Options struct {
	// Sources are the retrieval sources to search; empty searches every
	// source that is not optional (Volumes and Chronicle memory).
	Sources          []string `json:"sources,omitempty"`
	LexicalSemantic  int      `json:"lexicalSemantic,omitempty"`  // −5 … +5
	ChronicleLibrary int      `json:"chronicleLibrary,omitempty"` // −5 … +5
	TopK             int      `json:"topK,omitempty"`             // 0 leaves it to the caller
	// VolumeIDs narrow the library to these Volumes; empty searches every
	// Volume the principal may read.
	VolumeIDs []uint64 `json:"-"`
}

type optionsKey struct{}

// WithOptions is ctx whose tool searches run with o.
func WithOptions(ctx context.Context, o Options) context.Context {
	return context.WithValue(ctx, optionsKey{}, o)
}

// OptionsFrom are the options ctx carries, and whether it carries any.
func OptionsFrom(ctx context.Context) (Options, bool) {
	if ctx == nil {
		return Options{}, false
	}
	o, ok := ctx.Value(optionsKey{}).(Options)
	return o, ok
}

// ParseOptions reads options from data, as a graph passes them:
// {sources, lexicalSemantic, chronicleLibrary, topK, volumeIds}, Volume ids
// as text or numbers. Nil or an empty object is no options (false).
func ParseOptions(v any) (Options, bool, error) {
	var raw json.RawMessage
	switch x := v.(type) {
	case nil:
		return Options{}, false, nil
	case json.RawMessage:
		raw = x
	case string:
		if strings.TrimSpace(x) == "" {
			return Options{}, false, nil
		}
		raw = json.RawMessage(x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return Options{}, false, err
		}
		raw = b
	}
	var in struct {
		Options
		VolumeIDs []any `json:"volumeIds"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Options{}, false, fmt.Errorf("the retrieval options are not an object: %w", err)
	}
	if string(raw) == "null" || string(raw) == "{}" {
		return Options{}, false, nil
	}
	o := in.Options
	for _, id := range in.VolumeIDs {
		n, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(id)), 10, 64)
		if err != nil || n == 0 {
			return Options{}, false, fmt.Errorf("volume id %v is not an id", id)
		}
		o.VolumeIDs = append(o.VolumeIDs, n)
	}
	switch {
	case o.LexicalSemantic < -5 || o.LexicalSemantic > 5 || o.ChronicleLibrary < -5 || o.ChronicleLibrary > 5:
		return Options{}, false, fmt.Errorf("the dials go from -5 to 5")
	case o.TopK < 0 || o.TopK > 50:
		return Options{}, false, fmt.Errorf("top-K must be between 1 and 50")
	}
	return o, true, nil
}
