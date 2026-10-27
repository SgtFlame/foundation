package cognition

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func types() []NodeType {
	return []NodeType{
		{Descriptor: extensions.Descriptor{ID: "start"}, Title: "Start", Event: true,
			Pins: Pins{Out: []Pin{ExecPin(Then, ""), DataPin("s", "S", "string", "")}}},
		{Descriptor: extensions.Descriptor{ID: "step"}, Title: "Step",
			Pins: Pins{In: []Pin{ExecPin(Then, ""), DataPin("s", "S", "string", "")}, Out: []Pin{ExecPin(Then, ""), DataPin("n", "N", "number", "")}}},
		{Descriptor: extensions.Descriptor{ID: "pure"}, Title: "Pure",
			Pins: Pins{In: []Pin{DataPin("x", "X", Any, "")}, Out: []Pin{DataPin("y", "Y", "string", "")}}},
	}
}

func e(from, fromPin, to, toPin string) Edge {
	return Edge{From: PinRef{from, fromPin}, To: PinRef{to, toPin}}
}

func messages(r Resolved) string {
	var out []string
	for _, p := range r.Problems {
		out = append(out, p.Severity+" "+p.NodeID+": "+p.Message)
	}
	return strings.Join(out, "\n")
}

func TestResolveAcceptsAValidGraph(t *testing.T) {
	g := Graph{Nodes: []Node{{ID: "s", Type: "start"}, {ID: "a", Type: "step"}, {ID: "p", Type: "pure"}},
		Edges: []Edge{e("s", "then", "a", "then"), e("s", "s", "p", "x"), e("p", "y", "a", "s")}}
	if r := Resolve(g, types()); len(r.Problems) > 0 {
		t.Errorf("problems:\n%s", messages(r))
	}
}

func TestResolveFindsProblems(t *testing.T) {
	g := Graph{
		Nodes: []Node{{ID: "a", Type: "step"}, {ID: "b", Type: "step"}, {ID: "x", Type: "nope"}, {ID: "p", Type: "pure"}, {ID: "q", Type: "pure"}},
		Edges: []Edge{
			e("a", "n", "b", "s"),                        // number into string
			e("a", "then", "b", "s"),                     // exec into data
			e("p", "y", "q", "x"), e("q", "y", "p", "x"), // a pure cycle
			e("a", "nope", "b", "then"),
		},
	}
	got := messages(Resolve(g, types()))
	for _, want := range []string{
		"no event node", `no node type "nope"`, "N carries a number, but S takes a string", "an exec pin is joined to a data pin",
		"cycle of pure nodes", `no output "nope"`, "warning a: no event reaches",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestResolveRequiresInputs(t *testing.T) {
	g := Graph{Nodes: []Node{{ID: "s", Type: "start"}, {ID: "a", Type: "step"}}, Edges: []Edge{e("s", "then", "a", "then")}}
	if got := messages(Resolve(g, types())); !strings.Contains(got, "the input S is not connected") {
		t.Errorf("problems:\n%s", got)
	}
}
