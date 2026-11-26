package retrievalcontext

import (
	"encoding/json"
	"slices"
	"testing"
)

// A chat round's kb.search searches as its RAG Search does (forge 15a).
func TestOptionsOf(t *testing.T) {
	o := OptionsOf(json.RawMessage(`{"lexicalSemantic": 2, "chronicleLibrary": -9, "topK": 5, "sources": {"library": true, "chronicle": false}}`))
	if o.LexicalSemantic != 2 || o.ChronicleLibrary != -5 || o.TopK != 5 || !slices.Equal(o.Sources, []string{"none", "library"}) {
		t.Errorf("%+v", o)
	}
	if o := OptionsOf(nil); o.Sources != nil || o.TopK != 0 {
		t.Errorf("no settings: %+v", o)
	}
}
