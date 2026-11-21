package prefabs_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// hasProblem reports whether a refusal has a problem at path whose message
// holds text.
func hasProblem(problems []live.Problem, path, text string) bool {
	return slices.ContainsFunc(problems, func(p live.Problem) bool { return p.Path == path && strings.Contains(p.Message, text) })
}

// Variants (forge 04f): named prop presets an instance picks with its
// node's variant, its own props winning; checked, rendered, previewed,
// and breaking when removed while used.
func TestVariants(t *testing.T) {
	st := newRender(t)
	st.do("prefab.create", "tag", map[string]any{"name": "Tag"})
	st.do("prefab.setInterface", "tag", map[string]any{"props": map[string]any{
		"label": map[string]any{"type": "string", "required": true},
		"color": map[string]any{"type": "enum", "options": []string{"default", "primary", "success"}, "default": "default"},
		"look":  map[string]any{"type": "enum", "options": []string{"filled", "outlined"}, "default": "filled"},
	}})
	st.insert("tag", "", map[string]any{"type": "mui.chip", "key": "chip", "bind": map[string]any{"label": "props.label", "color": "props.color", "variant": "props.look"}})

	// A variant presets declared props only, with values that fit them
	res := st.try("prefab.setVariants", "tag", map[string]any{"variants": map[string]any{"bad": map[string]any{"props": map[string]any{"size": "small", "color": "red"}}}})
	if res.Common.Code != 422 || !hasProblem(res.Problems, "prefab.variants.bad.props.size", "no prop") || !hasProblem(res.Problems, "prefab.variants.bad.props.color", "does not fit") {
		t.Fatalf("a bad variant was not refused: %d %+v", res.Common.Code, res.Problems)
	}
	st.do("prefab.setVariants", "tag", map[string]any{"variants": map[string]any{
		"loud": map[string]any{"description": "Stands out", "props": map[string]any{"color": "primary", "look": "outlined"}},
		"done": map[string]any{"props": map[string]any{"color": "success", "label": "Done"}},
		"old":  map[string]any{"props": map[string]any{"look": "outlined"}},
	}})
	st.publish("tag")

	// Instances pick a variant; one presetting a required prop needs no
	// value of its own; an unknown variant, or one on a primitive, is refused
	st.do("prefab.create", "tags", map[string]any{"name": "Tags"})
	st.insert("tags", "", map[string]any{"type": "mui.stack", "key": "root"})
	st.insert("tags", "root", map[string]any{"type": "prefab.tag", "key": "loud", "variant": "loud", "props": map[string]any{"label": "Hi"}})
	st.insert("tags", "root", map[string]any{"type": "prefab.tag", "key": "mine", "variant": "loud", "props": map[string]any{"label": "Mine", "color": "success"}})
	st.insert("tags", "root", map[string]any{"type": "prefab.tag", "key": "done", "variant": "done"})
	res = st.try("prefab.insertNode", "tags", map[string]any{"parentPath": "root", "node": map[string]any{"type": "prefab.tag", "key": "x", "variant": "quiet", "props": map[string]any{"label": "x"}}})
	if res.Common.Code != 422 || !hasProblem(res.Problems, "prefab.template.root/x.variant", `no variant "quiet"`) {
		t.Errorf("an unknown variant was not refused: %+v", res.Problems)
	}
	res = st.try("prefab.updateNode", "tags", map[string]any{"path": "root", "variant": "loud"})
	if res.Common.Code != 422 || !hasProblem(res.Problems, "prefab.template.root.variant", "has no variants") {
		t.Errorf("a variant on a primitive was not refused: %+v", res.Problems)
	}
	// updateNode picks one, and null clears it
	st.do("prefab.updateNode", "tags", map[string]any{"path": "root/mine", "variant": "old"})
	st.do("prefab.updateNode", "tags", map[string]any{"path": "root/mine", "variant": nil})
	st.do("prefab.updateNode", "tags", map[string]any{"path": "root/mine", "variant": "loud"})
	st.publish("tags")

	// Rendered: the variant's props, under the instance's own
	p := ui.PrefabPage("tags", "", nil)
	p.ID, p.App, p.Title = "test/tags", "test", "Tags"
	tree := st.open(p).tree()
	for _, c := range []struct{ key, label, color, look string }{
		{"loud", "Hi", "primary", "outlined"}, {"mine", "Mine", "success", "outlined"}, {"done", "Done", "success", "filled"},
	} {
		n := tree.Child(c.key)
		if n == nil || n.Props["label"] != c.label || n.Props["color"] != c.color || n.Props["variant"] != c.look {
			t.Errorf("%s should be %s/%s/%s: %s", c.key, c.label, c.color, c.look, text(n))
		}
	}
	// A page and a preview take a variant too; an unknown one is a problem
	p = ui.PrefabPage("tag", "done", nil)
	p.ID, p.App, p.Title = "test/tag", "test", "Tag"
	if n := st.open(p).tree(); n.Props["label"] != "Done" || n.Props["color"] != "success" {
		t.Errorf("the page's variant: %s", text(n))
	}
	o := ui.Options{Catalog: ui.Catalog(st.reg), Subs: st.subs}
	if tree, problems, _ := ui.Preview(user(), o, "tag", "loud", map[string]any{"label": "P"}, false); len(problems) > 0 || tree.Props["color"] != "primary" {
		t.Errorf("the preview's variant: %+v %s", problems, text(tree))
	}
	if _, problems, _ := ui.Preview(user(), o, "tag", "quiet", map[string]any{"label": "P"}, false); len(problems) != 1 || !strings.Contains(problems[0].Message, `no variant "quiet"`) {
		t.Errorf("an unknown variant should be a problem: %+v", problems)
	}
	// The catalog describes a prefab with an example per variant
	if c := ui.PrefabComponent(ui.Prefab{ID: "tag", Name: "Tag", Variants: map[string]ui.VariantDecl{"a": {}, "b": {}}}); len(c.Examples) != 3 || !strings.Contains(string(c.Examples[1]), `"variant":"a"`) {
		t.Errorf("the component's examples: %s", c.Examples)
	}

	// Removing a variant a published prefab uses breaks it; one no one
	// uses does not
	st.do("prefab.setVariants", "tag", map[string]any{"variants": map[string]any{"done": map[string]any{"props": map[string]any{"color": "success", "label": "Done"}}}})
	res = st.try("prefab.publish", "tag", map[string]int{"rev": st.rev(prefabs.DraftProjector, "prefab.tag", "tag")})
	if res.Common.Code != 422 || len(res.Problems) != 1 || res.Problems[0].Code != "prefab_breaking" || res.Problems[0].Path != "prefab.variants.loud" ||
		!strings.Contains(res.Problems[0].Message, "tags at template.root/loud") || !strings.Contains(res.Problems[0].Message, "tags at template.root/mine") {
		t.Errorf("removing a used variant should break tags: %+v", res.Problems)
	}
	st.do("prefab.setVariants", "tag", map[string]any{"variants": map[string]any{
		"loud": map[string]any{"props": map[string]any{"color": "primary"}},
		"done": map[string]any{"props": map[string]any{"color": "success", "label": "Done"}},
	}})
	st.publish("tag")
}
