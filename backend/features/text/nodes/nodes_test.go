package textnodes_test

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	textnodes "github.com/SgtFlame/foundation/backend/features/text/nodes"
)

// Anchor resolve's Quotes find many quotes in one text (forge 18c).
func TestAnchorQuotes(t *testing.T) {
	reg := extensions.NewRegistry()
	textnodes.Contribute(reg)
	var nt cognition.NodeType
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == textnodes.AnchorID {
			nt = n
		}
	}
	text := "# Booking\n\nA table holds four.\n\n# Deposits\n\nA table holds four at lunch.\n"
	x := &cognition.Exec{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}}
	out, _, err := nt.Run(x, cognition.Values{"text": text, "quotes": []any{
		map[string]any{"id": "t1", "quote": "holds four", "headingPath": []any{"Deposits"}},
		map[string]any{"id": "t2", "quote": "a table holds  four."},
		map[string]any{"id": "t3", "quote": "children eat free"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	anchors := out["anchors"].([]any)
	if len(anchors) != 3 {
		t.Fatalf("anchors = %v", anchors)
	}
	a1, a2, a3 := anchors[0].(map[string]any), anchors[1].(map[string]any), anchors[2].(map[string]any)
	if a1["id"] != "t1" || a1["found"] != true || a1["exact"] != true || a1["headingPath"].([]any)[0] != "Deposits" {
		t.Errorf("t1 = %v", a1)
	}
	if a2["found"] != true || a2["exact"] != false || a2["headingPath"].([]any)[0] != "Booking" {
		t.Errorf("t2 = %v", a2)
	}
	if a3["found"] != false {
		t.Errorf("t3 = %v", a3)
	}
	if out["found"] != false {
		t.Errorf("no quote given, found = %v", out["found"])
	}
}
