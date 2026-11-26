package cognitionnodes

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// Expression is the Expression node's type (Blueprints question 2:
// expr-lang/expr, typed, sandboxed, with no I/O).
const Expression = "data.expression"

var builtins = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range builtin.Builtins {
		m[f.Name] = true
	}
	return m
}()

// Identifiers are the free names an expression reads, in order: its
// Expression node's input pins. Builtins and names it declares with let are
// not inputs, unless used as names: an input named count shadows count().
func Identifiers(src string) ([]string, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	tree, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	v := &idents{declared: map[string]bool{}, callees: map[ast.Node]bool{}}
	ast.Walk(&tree.Node, v)
	var out []string
	seen := map[string]bool{}
	for _, n := range v.found {
		id := n.Value
		if v.callees[n] || seen[id] || v.declared[id] || id == "true" || id == "false" || id == "nil" {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// idents gathers identifiers; the walk is post-order, so call names are
// told apart after it.
type idents struct {
	found    []*ast.IdentifierNode
	declared map[string]bool
	callees  map[ast.Node]bool
}

func (v *idents) Visit(n *ast.Node) {
	switch t := (*n).(type) {
	case *ast.VariableDeclaratorNode:
		v.declared[t.Name] = true
	case *ast.CallNode:
		v.callees[t.Callee] = true
	case *ast.IdentifierNode:
		v.found = append(v.found, t)
	}
}

// programs caches compiled expressions: an expression is compiled once,
// the first time a published version runs it.
var programs sync.Map // source → *vm.Program

func compile(src string) (*vm.Program, error) {
	if p, ok := programs.Load(src); ok {
		return p.(*vm.Program), nil
	}
	ids, err := Identifiers(src)
	if err != nil {
		return nil, err
	}
	opts := []expr.Option{expr.AllowUndefinedVariables()}
	for _, id := range ids {
		if builtins[id] {
			opts = append(opts, expr.DisableBuiltin(id)) // the input shadows it
		}
	}
	p, err := expr.Compile(src, opts...)
	if err != nil {
		return nil, err
	}
	programs.Store(src, p)
	return p, nil
}

// Eval runs an expression over an environment of values.
func Eval(src string, env map[string]any) (any, error) {
	p, err := compile(src)
	if err != nil {
		return nil, err
	}
	return expr.Run(p, env)
}

type exprConfig struct {
	Expr string `json:"expr"`
	Type string `json:"type"` // the result's type; any when empty
}

func exprOf(config json.RawMessage) (exprConfig, error) {
	var c exprConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return c, err
		}
	}
	if c.Type == "" {
		c.Type = cognition.Any
	}
	return c, nil
}

// inputPins are data inputs named by ids, of any type.
func inputPins(ids []string, description string) []cognition.Pin {
	pins := make([]cognition.Pin, len(ids))
	for i, id := range ids {
		pins[i] = cognition.Pin{ID: id, Name: id, Type: cognition.Any, Optional: true, Description: description}
	}
	return pins
}

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func templateNames(t string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(t, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func number(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case uint64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(t, 64)
	case nil:
		return 0, nil
	}
	return 0, fmt.Errorf("%v is not a number", v)
}

// normalize turns Go values into their JSON forms, so a path reads maps
// of any struct or slice.
func normalize(v any) any {
	switch v.(type) {
	case map[string]any, []any, string, float64, bool, nil:
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// GetPath reads a dotted path ("a.b.0.c") from a JSON-like value.
func GetPath(v any, path string) any {
	if path == "" {
		return v
	}
	for _, part := range strings.Split(path, ".") {
		switch t := normalize(v).(type) {
		case map[string]any:
			v = t[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

// SetPath is v with path set to value, copying what it changes.
func SetPath(v any, path string, value any) any {
	if path == "" {
		return value
	}
	head, rest, _ := strings.Cut(path, ".")
	m, _ := normalize(v).(map[string]any)
	out := make(map[string]any, len(m)+1)
	for k, x := range m {
		out[k] = x
	}
	out[head] = SetPath(m[head], rest, value)
	return out
}

func list(in cognition.Values, pin string) ([]any, error) {
	v := normalize(in[pin])
	if v == nil {
		return []any{}, nil
	}
	l, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not a list", pin)
	}
	return l, nil
}

func contributeDataNodes(reg *extensions.Registry) {
	exprSchema := json.RawMessage(`{"type":"object","properties":{"expr":{"type":"string","title":"Expression","description":"An expr-lang expression over the item (item, index)"}}}`)
	listPin := cognition.DataPin("list", "List", cognition.Any, "The items, a JSON array")

	node(reg, 300, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: Expression},
		Title:       "Expression",
		Category:    "Data",
		Description: "Computes a value from its inputs with a small, sandboxed expression (expr-lang: arithmetic, comparisons, strings, ?:, ??, lists and maps). Each name the expression uses becomes an input pin. It cannot do I/O.",
		Example:     `count + 1, "Hello " + name, or source != target`,
		Config:      json.RawMessage(`{"type":"object","properties":{"expr":{"type":"string","title":"Expression"},"type":{"type":"string","title":"Result type","description":"The output's type; any when empty"}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			c, err := exprOf(config)
			if err != nil {
				return cognition.Pins{}, err
			}
			ids, err := Identifiers(c.Expr)
			if err != nil {
				return cognition.Pins{}, fmt.Errorf("the expression is not valid: %w", err)
			}
			return cognition.Pins{In: inputPins(ids, "A value the expression reads"), Out: []cognition.Pin{cognition.DataPin("value", "Value", c.Type, "The expression's value")}}, nil
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			c, err := exprOf(x.Node.Config)
			if err != nil {
				return nil, "", err
			}
			env := map[string]any{}
			for k, v := range in {
				env[k] = normalize(v)
			}
			v, err := Eval(c.Expr, env)
			if err != nil {
				return nil, "", fmt.Errorf("the expression failed: %w", err)
			}
			return cognition.Values{"value": v}, "", nil
		},
	})

	fieldsSchema := json.RawMessage(`{"type":"object","properties":{"fields":{"type":"array","title":"Fields","items":{"type":"string"}}}}`)
	node(reg, 310, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.makeObject"}, Title: "Make object", Category: "Data",
		Description: "Makes a JSON object with one field per input",
		Example:     "Make object with fields source and target, as a command's args",
		Config:      fieldsSchema,
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c struct{ Fields []string }
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			return cognition.Pins{In: inputPins(c.Fields, "The field's value"), Out: []cognition.Pin{cognition.DataPin("object", "Object", "json", "The object")}}, nil
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			obj := map[string]any{}
			for k, v := range in {
				obj[k] = normalize(v)
			}
			return cognition.Values{"object": obj}, "", nil
		},
	})
	pathSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","title":"Path","description":"Dotted, list items by index: a.b.0"}}}`)
	node(reg, 320, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.getField"}, Title: "Get field", Category: "Data",
		Description: "Reads a field of an object at a dotted path",
		Example:     "Get field value of the counter's state",
		Config:      pathSchema,
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("object", "Object", cognition.Any, "The object to read")},
			Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The field's value; nothing when absent")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"value": GetPath(in["object"], c.Path)}, "", nil
		},
	})
	node(reg, 330, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.setField"}, Title: "Set field", Category: "Data",
		Description: "A copy of an object with a field at a dotted path set",
		Example:     "Set field greeting of the view state to the new text",
		Config:      pathSchema,
		Pins: cognition.Pins{
			In:  []cognition.Pin{{ID: "object", Name: "Object", Type: cognition.Any, Optional: true, Description: "The object; an empty one when unconnected"}, cognition.DataPin("value", "Value", cognition.Any, "The field's new value")},
			Out: []cognition.Pin{cognition.DataPin("object", "Object", "json", "The changed copy")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Path string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			return cognition.Values{"object": SetPath(in["object"], c.Path, normalize(in["value"]))}, "", nil
		},
	})

	each := func(order int, id, title, description, example string, out cognition.Pin, fn func(src string, items []any) (any, error)) {
		node(reg, order, cognition.NodeType{
			Descriptor: extensions.Descriptor{ID: id}, Title: title, Category: "Data", Description: description, Example: example,
			Config: exprSchema,
			Pins:   cognition.Pins{In: []cognition.Pin{listPin}, Out: []cognition.Pin{out}},
			Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
				var c exprConfig
				if err := x.Config(&c); err != nil {
					return nil, "", err
				}
				items, err := list(in, "list")
				if err != nil {
					return nil, "", err
				}
				v, err := fn(c.Expr, items)
				return cognition.Values{out.ID: v}, "", err
			},
		})
	}
	evalItem := func(src string, item any, i int) (any, error) {
		return Eval(src, map[string]any{"item": item, "index": i})
	}
	each(340, "data.map", "Map", "Each item turned into the expression's value over it (item, index)", "Map item.label over the graph's nodes",
		cognition.DataPin("list", "List", "json", "The new items"), func(src string, items []any) (any, error) {
			out := make([]any, len(items))
			for i, it := range items {
				v, err := evalItem(src, it, i)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return out, nil
		})
	each(350, "data.filter", "Filter", "The items for which the expression is true (item, index)", "Filter the edges with item.source == id",
		cognition.DataPin("list", "List", "json", "The items kept"), func(src string, items []any) (any, error) {
			out := []any{}
			for i, it := range items {
				v, err := evalItem(src, it, i)
				if err != nil {
					return nil, err
				}
				if b, _ := v.(bool); b {
					out = append(out, it)
				}
			}
			return out, nil
		})
	each(360, "data.sort", "Sort", "The items sorted by the expression's value over each (item); numbers and text sort as such", "Sort deals by item.amount",
		cognition.DataPin("list", "List", "json", "The items, sorted"), func(src string, items []any) (any, error) {
			keys := make([]any, len(items))
			for i, it := range items {
				v, err := evalItem(src, it, i)
				if err != nil {
					return nil, err
				}
				keys[i] = v
			}
			idx := make([]int, len(items))
			for i := range idx {
				idx[i] = i
			}
			sort.SliceStable(idx, func(a, b int) bool {
				x, errX := number(keys[idx[a]])
				y, errY := number(keys[idx[b]])
				if errX == nil && errY == nil {
					return x < y
				}
				return text(keys[idx[a]]) < text(keys[idx[b]])
			})
			out := make([]any, len(items))
			for i, j := range idx {
				out[i] = items[j]
			}
			return out, nil
		})
	each(370, "data.group", "Group", "The items grouped by the expression's value over each (item): an object from each key to its items", "Group deals by item.stage",
		cognition.DataPin("groups", "Groups", "json", "Each key's items"), func(src string, items []any) (any, error) {
			out := map[string]any{}
			for i, it := range items {
				v, err := evalItem(src, it, i)
				if err != nil {
					return nil, err
				}
				k := text(v)
				l, _ := out[k].([]any)
				out[k] = append(l, it)
			}
			return out, nil
		})
	node(reg, 380, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.reduce"}, Title: "Reduce", Category: "Data",
		Description: "Folds the items into one value: the expression gets acc (the value so far, starting with Initial), item and index",
		Example:     "Reduce with acc + item.amount and Initial 0 to total the deals",
		Config:      exprSchema,
		Pins: cognition.Pins{
			In:  []cognition.Pin{listPin, {ID: "initial", Name: "Initial", Type: cognition.Any, Optional: true, Description: "The starting value"}},
			Out: []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "The folded value")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c exprConfig
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			items, err := list(in, "list")
			if err != nil {
				return nil, "", err
			}
			acc := normalize(in["initial"])
			for i, it := range items {
				if acc, err = Eval(c.Expr, map[string]any{"acc": acc, "item": it, "index": i}); err != nil {
					return nil, "", err
				}
			}
			return cognition.Values{"value": acc}, "", nil
		},
	})

	node(reg, 390, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.template"}, Title: "Template string", Category: "Data",
		Description: "Text with {{name}} placeholders; each placeholder becomes an input pin",
		Example:     "Hello, {{name}}! with the name the user typed",
		Config:      json.RawMessage(`{"type":"object","properties":{"template":{"type":"string","title":"Template"}}}`),
		PinsFor: func(config json.RawMessage) (cognition.Pins, error) {
			var c struct{ Template string }
			if len(config) > 0 {
				if err := json.Unmarshal(config, &c); err != nil {
					return cognition.Pins{}, err
				}
			}
			return cognition.Pins{In: inputPins(templateNames(c.Template), "The placeholder's value"), Out: []cognition.Pin{cognition.DataPin("text", "Text", "string", "The filled-in text")}}, nil
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Template string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			out := placeholder.ReplaceAllStringFunc(c.Template, func(m string) string {
				return text(in[placeholder.FindStringSubmatch(m)[1]])
			})
			return cognition.Values{"text": out}, "", nil
		},
	})
	node(reg, 400, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.compare"}, Title: "Compare", Category: "Data",
		Description: "Compares two values: ==, !=, <, <=, > or >=; numbers as numbers, the rest as text",
		Example:     "Compare count >= 10 to show a warning",
		Config:      json.RawMessage(`{"type":"object","properties":{"op":{"type":"enum","title":"Operator","enum":["==","!=","<","<=",">",">="]}}}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{{ID: "a", Name: "A", Type: cognition.Any, Optional: true, Description: "The left value"}, {ID: "b", Name: "B", Type: cognition.Any, Optional: true, Description: "The right value"}},
			Out: []cognition.Pin{cognition.DataPin("result", "Result", "bool", "Whether the comparison holds")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Op string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			cmp := 0
			a, errA := number(in["a"])
			b, errB := number(in["b"])
			if errA == nil && errB == nil {
				cmp = map[bool]int{true: -1, false: 0}[a < b] + map[bool]int{true: 1, false: 0}[a > b]
			} else {
				cmp = strings.Compare(text(normalize(in["a"])), text(normalize(in["b"])))
			}
			var r bool
			switch c.Op {
			case "", "==":
				r = cmp == 0
			case "!=":
				r = cmp != 0
			case "<":
				r = cmp < 0
			case "<=":
				r = cmp <= 0
			case ">":
				r = cmp > 0
			case ">=":
				r = cmp >= 0
			default:
				return nil, "", fmt.Errorf("unknown operator %q", c.Op)
			}
			return cognition.Values{"result": r}, "", nil
		},
	})
	node(reg, 410, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.math"}, Title: "Math", Category: "Data",
		Description: "Arithmetic on two numbers: +, -, *, /, %, min or max",
		Example:     "Math + with the counter's value and 1",
		Config:      json.RawMessage(`{"type":"object","properties":{"op":{"type":"enum","title":"Operator","enum":["+","-","*","/","%","min","max"]}}}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{{ID: "a", Name: "A", Type: "number", Optional: true, Description: "The left number"}, {ID: "b", Name: "B", Type: "number", Optional: true, Description: "The right number"}},
			Out: []cognition.Pin{cognition.DataPin("result", "Result", "number", "The result")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Op string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			a, err := number(in["a"])
			if err != nil {
				return nil, "", err
			}
			b, err := number(in["b"])
			if err != nil {
				return nil, "", err
			}
			var r float64
			switch c.Op {
			case "", "+":
				r = a + b
			case "-":
				r = a - b
			case "*":
				r = a * b
			case "/":
				if b == 0 {
					return nil, "", fmt.Errorf("division by zero")
				}
				r = a / b
			case "%":
				r = math.Mod(a, b)
			case "min":
				r = math.Min(a, b)
			case "max":
				r = math.Max(a, b)
			default:
				return nil, "", fmt.Errorf("unknown operator %q", c.Op)
			}
			return cognition.Values{"result": r}, "", nil
		},
	})
	node(reg, 420, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.format"}, Title: "Format", Category: "Data",
		Description: "Formats a value as text: a date (Go layout, RFC 3339 by default), a number with decimals, or money with a currency",
		Example:     "Format money 1234.5 with currency USD as USD 1,234.50",
		Config: json.RawMessage(`{"type":"object","properties":{"as":{"type":"enum","title":"As","enum":["date","number","money"]},
			"layout":{"type":"string","title":"Date layout"},"decimals":{"type":"integer","title":"Decimals"},"currency":{"type":"string","title":"Currency"}}}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("value", "Value", cognition.Any, "A date (RFC 3339 text) or a number")},
			Out: []cognition.Pin{cognition.DataPin("text", "Text", "string", "The formatted text")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				As, Layout, Currency string
				Decimals             *int
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			switch c.As {
			case "date":
				t, err := time.Parse(time.RFC3339, text(in["value"]))
				if err != nil {
					return nil, "", err
				}
				if c.Layout == "" {
					c.Layout = time.RFC3339
				}
				return cognition.Values{"text": t.Format(c.Layout)}, "", nil
			default:
				n, err := number(in["value"])
				if err != nil {
					return nil, "", err
				}
				d := 2
				if c.Decimals != nil {
					d = *c.Decimals
				} else if c.As != "money" {
					d = -1
				}
				s := groupThousands(strconv.FormatFloat(n, 'f', d, 64))
				if c.As == "money" && c.Currency != "" {
					s = c.Currency + " " + s
				}
				return cognition.Values{"text": s}, "", nil
			}
		},
	})
	node(reg, 430, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.parse"}, Title: "Parse", Category: "Data",
		Description: "Reads JSON text into a value",
		Example:     `Parse {"by": 2} into an object`,
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.DataPin("text", "Text", "string", "JSON text")},
			Out: []cognition.Pin{cognition.DataPin("value", "Value", "json", "The value")},
		},
		Run: func(_ *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var v any
			if err := json.Unmarshal([]byte(text(in["text"])), &v); err != nil {
				return nil, "", fmt.Errorf("the text is not JSON: %w", err)
			}
			return cognition.Values{"value": v}, "", nil
		},
	})
	node(reg, 440, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.now"}, Title: "Now", Category: "Data",
		Description: "The current time, as RFC 3339 text in UTC",
		Example:     "Now, stored as the time a deal was won",
		Volatile:    true,
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("time", "Time", "string", "The current time")}},
		Run: func(*cognition.Exec, cognition.Values) (cognition.Values, string, error) {
			return cognition.Values{"time": time.Now().UTC().Format(time.RFC3339Nano)}, "", nil
		},
	})
	node(reg, 450, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "data.newId"}, Title: "New id", Category: "Data",
		Description: "A new unique id (a TSID, as text)",
		Example:     "New id for a record about to be created",
		Volatile:    true,
		Pins:        cognition.Pins{Out: []cognition.Pin{cognition.DataPin("id", "ID", "string", "The new id")}},
		Run: func(*cognition.Exec, cognition.Values) (cognition.Values, string, error) {
			return cognition.Values{"id": strconv.FormatUint(features.NewTSID(), 10)}, "", nil
		},
	})
}

// groupThousands puts commas between thousands of a formatted number.
func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if hasFrac {
		return sign + b.String() + "." + frac
	}
	return sign + b.String()
}
