package ui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

type frames struct{ got []any }

func (f *frames) send(frame any) { f.got = append(f.got, frame) }

func (f *frames) last() PatchFrame {
	for i := len(f.got) - 1; i >= 0; i-- {
		if p, ok := f.got[i].(PatchFrame); ok {
			return p
		}
	}
	return PatchFrame{}
}

func testPage(renders map[string]int) Page {
	return Page{
		Descriptor: extensions.Descriptor{ID: "t/main"}, App: "t",
		OnOpen: func(h *Handle) error { h.Set("tab", "a"); h.Set("name", "x"); return nil },
		Render: func(r *Render) *Node {
			renders["root"]++
			return N("box", "root",
				r.Region("tabs", func(r *Render) *Node {
					renders["tabs"]++
					return N("text", "tabs").P("text", "tab "+r.State("tab").(string))
				}),
				N("text", "name").B("text", "state.name"),
			)
		},
		Handlers: map[string]func(h *Handle) error{
			"tab":  func(h *Handle) error { h.Set("tab", h.String("tab")); return nil },
			"name": func(h *Handle) error { h.Set("name", h.String("name")); return nil },
		},
	}
}

func TestRegionsAndBindingsPatchOnlyWhatChanged(t *testing.T) {
	renders := map[string]int{}
	f := &frames{}
	args := features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
	s, fail := Open(args, "s1", testPage(renders), json.RawMessage(`{}`), nil, Options{Send: f.send})
	if fail != nil {
		t.Fatal(fail)
	}
	tree := f.got[0].(TreeFrame).Tree
	if tree.At([]string{"name"}).Props["text"] != "x" {
		t.Fatalf("bound prop not resolved: %+v", tree.At([]string{"name"}))
	}

	s.Event(args, "name", []string{"name"}, map[string]any{"name": "y"})
	p := f.last()
	if len(p.Patches) != 1 || p.Patches[0].Op != "props" || p.Patches[0].Props["text"] != "y" || renders["tabs"] != 1 || renders["root"] != 1 {
		t.Fatalf("a bound prop should patch alone, without rendering: %+v %v", p.Patches, renders)
	}

	s.Event(args, "tab", []string{"tabs"}, map[string]any{"tab": "b"})
	p = f.last()
	if renders["tabs"] != 2 || renders["root"] != 1 {
		t.Fatalf("only the region should render again: %v", renders)
	}
	if len(p.Patches) != 1 || p.Patches[0].Op != "props" || p.Patches[0].Props["text"] != "tab b" {
		t.Fatalf("got %+v", p.Patches)
	}

	// An event for a node the server no longer has gets the tree back
	s.Event(args, "tab", []string{"gone"}, nil)
	if tf, ok := f.got[len(f.got)-1].(TreeFrame); !ok || !tf.Stale {
		t.Fatalf("want a stale tree, got %+v", f.got[len(f.got)-1])
	}
}

func TestDiffByKey(t *testing.T) {
	old := N("box", "r", N("a", "1"), N("a", "2"), N("a", "3"))
	next := N("box", "r", N("a", "3"), N("a", "1").P("x", 1), N("a", "4"))
	patches := Diff(nil, old, next)
	// Apply them the way the browser does and compare
	got := old.Clone()
	for _, p := range patches {
		apply(got, p)
	}
	if !equalJSON(got, next) {
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(next)
		t.Fatalf("patches %+v gave %s, want %s", patches, a, b)
	}
}

func apply(root *Node, p Patch) {
	switch p.Op {
	case "replace":
		parent := root.At(p.Path[:len(p.Path)-1])
		for i, c := range parent.Children {
			if c.Key == p.Path[len(p.Path)-1] {
				parent.Children[i] = p.Node
			}
		}
	case "remove":
		parent := root.At(p.Path[:len(p.Path)-1])
		for i, c := range parent.Children {
			if c.Key == p.Path[len(p.Path)-1] {
				parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
				break
			}
		}
	case "insert":
		parent := root.At(p.Path)
		parent.Children = append(parent.Children, nil)
		copy(parent.Children[p.Index+1:], parent.Children[p.Index:])
		parent.Children[p.Index] = p.Node
	case "move":
		parent := root.At(p.Path)
		var n *Node
		for i, c := range parent.Children {
			if c.Key == p.Key {
				n = c
				parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
				break
			}
		}
		parent.Children = append(parent.Children, nil)
		copy(parent.Children[p.Index+1:], parent.Children[p.Index:])
		parent.Children[p.Index] = n
	case "props":
		n := root.At(p.Path)
		if n.Props == nil {
			n.Props = map[string]any{}
		}
		for k, v := range p.Props {
			if v == nil {
				delete(n.Props, k)
			} else {
				n.Props[k] = v
			}
		}
	}
}
