package ui

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// The template constructs beside primitives, slot and prefab.<id> (forge
// 04b, documentation/ServerUI.md › Built in slice 26). They are nodes like
// any other, so prefab.* commands address and edit them by key paths:
//
//	{"type": "repeat", "key": "rows", "props": {"each": "query:openTasks", "as": "task", "key": "task._id"}, "children": [template]}
//	{"type": "when", "key": "editing", "props": {"test": "state.editing"}, "slots": {"then": [node], "else": [node]}}
const (
	RepeatType = "repeat"
	WhenType   = "when"
)

// MaxSubscriptions is how many queries one session may hold (forge 04b
// question 4); an instance whose queries go beyond it shows a problem.
const MaxSubscriptions = 200

// InstanceOf splits a prefab instance's type, prefab.<id> or
// prefab.<id>@<version>; version 0 follows the last published version.
func InstanceOf(typ string) (id string, version int, ok bool) {
	rest, ok := strings.CutPrefix(typ, PrefabPrefix)
	if !ok {
		return "", 0, false
	}
	id, v, pinned := strings.Cut(rest, "@")
	if pinned {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return id, -1, true
		}
		version = n
	}
	return id, version, true
}

// IsExpression reports whether a binding is an expression: "=" then expr.
func IsExpression(from string) bool { return strings.HasPrefix(from, "=") }

// Expression roots a binding expression may read beside a repeat's row
// names (forge 04b question 5). local is the browser's and cannot be
// read on the server: bind local.<path> directly.
var expressionRoots = []string{"props", "state", "queries", "app", "user", "slots"}

// expressionReads are the top-level names an expression reads, each with
// the first member it reads when that is a constant (state.editing →
// "state" "editing"), for tracking.
func expressionReads(src string) ([][2]string, error) {
	tree, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	var out [][2]string
	v := &readsVisitor{out: &out}
	ast.Walk(&tree.Node, v)
	return out, nil
}

type readsVisitor struct{ out *[][2]string }

func (v *readsVisitor) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.MemberNode:
		if id, ok := n.Node.(*ast.IdentifierNode); ok {
			if s, ok := n.Property.(*ast.StringNode); ok {
				*v.out = append(*v.out, [2]string{id.Value, s.Value})
			}
		}
	case *ast.IdentifierNode:
		*v.out = append(*v.out, [2]string{n.Value, ""})
	}
}

// checkExpression compiles a binding expression against the names it may
// read: the roots and rows.
func checkExpression(src string, rows []string) error {
	env := map[string]any{}
	for _, r := range expressionRoots {
		env[r] = map[string]any{}
	}
	for _, r := range rows {
		env[r] = map[string]any{}
	}
	if _, err := expr.Compile(src, expr.Env(env)); err != nil {
		return err
	}
	reads, err := expressionReads(src)
	if err != nil {
		return err
	}
	for _, r := range reads {
		if r[0] == "local" {
			return fmt.Errorf("an expression cannot read local, which only the browser has")
		}
	}
	return nil
}

// evalExpression runs a binding expression over env.
func evalExpression(src string, env map[string]any) (any, error) {
	prog, err := expr.Compile(src, expr.Env(env))
	if err != nil {
		return nil, err
	}
	return expr.Run(prog, env)
}

// The sx rule (forge 04b › Styling): a prefab's sx may use only theme
// tokens, so apps follow the workbench's light and dark themes.
var (
	sxSpacing = []string{"m", "mt", "mr", "mb", "ml", "mx", "my", "p", "pt", "pr", "pb", "pl", "px", "py", "gap", "rowGap", "columnGap"}
	sxPalette = []string{"color", "bgcolor", "borderColor"}
	sxRoles   = regexp.MustCompile(`^((primary|secondary|error|warning|info|success)\.(main|light|dark|contrastText)|text\.(primary|secondary|disabled)|background\.(default|paper)|action\.(active|hover|selected|disabled)|divider)$`)
	sxType    = []string{"h1", "h2", "h3", "h4", "h5", "h6", "subtitle1", "subtitle2", "body1", "body2", "caption", "overline", "button"}
)

// SxTokens describes the allowed sx, as a fix a model can act on.
const SxTokens = "sx takes only theme tokens: spacing (m, mt, mr, mb, ml, mx, my, p, pt, pr, pb, pl, px, py, gap, rowGap, columnGap) as theme spacing units 0-16; " +
	"color, bgcolor and borderColor as palette roles (primary.main, secondary.light, error.dark, text.secondary, background.paper, action.hover, divider, …); typography as a variant (h1-h6, subtitle1, subtitle2, body1, body2, caption, overline, button)"

// CheckSx reports what in an sx value is not a theme token.
func CheckSx(sx any) []string {
	m, ok := sx.(map[string]any)
	if !ok {
		return []string{"sx must be an object"}
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		switch {
		case slices.Contains(sxSpacing, k):
			f, isNum := v.(float64)
			if n, isInt := v.(int); isInt {
				f, isNum = float64(n), true
			}
			auto := v == "auto" && (k[0] == 'm')
			if !auto && (!isNum || f < 0 || f > 16 || f*2 != float64(int(f*2))) {
				out = append(out, fmt.Sprintf("sx.%s: %v is not a spacing unit", k, jsonText(v)))
			}
		case slices.Contains(sxPalette, k):
			if s, _ := v.(string); !sxRoles.MatchString(s) {
				out = append(out, fmt.Sprintf("sx.%s: %v is not a palette role", k, jsonText(v)))
			}
		case k == "typography":
			if s, _ := v.(string); !slices.Contains(sxType, s) {
				out = append(out, fmt.Sprintf("sx.typography: %v is not a typography variant", jsonText(v)))
			}
		default:
			out = append(out, fmt.Sprintf("sx.%s is not a theme token", k))
		}
	}
	return out
}

func jsonText(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// PrefabComponent describes a published prefab as a component of the
// catalog, prefab.<id>: its props as the props schema, its events and
// slots, and no children (content goes in its slots).
func PrefabComponent(p Prefab) Component {
	props := map[string]any{}
	var required []string
	for _, k := range slices.Sorted(maps.Keys(p.Props)) {
		d := p.Props[k]
		props[k] = propSchema(d.Type, d.Options, d.Description)
		if d.Required {
			required = append(required, k)
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, _ := json.Marshal(schema)
	c := Component{Title: p.Name, Description: p.Description, Category: "Prefabs", Props: raw, Slots: slices.Sorted(maps.Keys(p.Slots))}
	c.ID = PrefabPrefix + p.ID
	if c.Description == "" {
		c.Description = "The prefab " + p.Name
	}
	for _, e := range slices.Sorted(maps.Keys(p.Events)) {
		d := p.Events[e]
		desc := d.Description
		if desc == "" {
			desc = "Raised by " + p.Name
		}
		if len(d.Payload) > 0 {
			raw, _ := json.Marshal(d.Payload)
			desc += "; payload " + string(raw)
		}
		c.Events = append(c.Events, Event{Name: e, Description: desc})
	}
	ex, _ := json.Marshal(map[string]any{"type": c.ID, "key": p.ID, "props": p.Preview.Props})
	c.Examples = []json.RawMessage{ex}
	if p.Owner != "" {
		c.Tags = []string{p.Owner}
	}
	return c
}

func propSchema(typ string, options []string, description string) map[string]any {
	s := map[string]any{}
	switch typ {
	case "string", "date", "dateTime":
		s["type"] = "string"
	case "number":
		s["type"] = "number"
	case "integer":
		s["type"] = "integer"
	case "bool":
		s["type"] = "boolean"
	case "enum":
		s["type"], s["enum"] = "string", options
	case "record":
		s["type"] = "object"
	case "records":
		s["type"] = "array"
	}
	if description != "" {
		s["description"] = description
	}
	return s
}
