package uicomponents

import (
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Every component's schema parses and its example validates.
func TestExamplesValidate(t *testing.T) {
	reg := extensions.NewRegistry()
	if err := Contribute(reg); err != nil {
		t.Fatal(err)
	}
	catalog := ui.Catalog(reg)
	for _, c := range catalog {
		var n ui.Node
		if err := json.Unmarshal(c.Examples[0], &n); err != nil {
			t.Fatalf("%s: example: %v", c.ID, err)
		}
		if p := ui.Validate(catalog, &n); len(p) > 0 {
			t.Errorf("%s: example has problems %+v", c.ID, p)
		}
	}
	if p := ui.Validate(catalog, ui.N("mui.nope", "x")); len(p) != 1 {
		t.Errorf("unknown component: %+v", p)
	}
	if p := ui.Validate(catalog, ui.N("mui.heading", "h").P("text", 3)); len(p) != 1 {
		t.Errorf("invalid prop: %+v", p)
	}
}
