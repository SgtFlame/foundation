package records

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

// Computed fields (Records.md › Built in slice 24) keep values no command
// sets: the records' decisions write them into their events (Forge
// 03-DataModel question 9).
//   - A formula is an expr-lang expression over the record's fields by id
//     (quantity * price). Decimals and money with a currency are numbers
//     in it; a field without a value is nil, and a formula that fails to
//     run has no value. A formula may read the fields above it, formulas
//     included.
//   - A rollup aggregates a field of the records a relation field points
//     to; a count needs no field.
//   - autoNumber counts up from 1 per entity type; createdAt, updatedAt and
//     createdBy are the record's _createdAt, _updatedAt and _createdBy.
//
// A formula's and a rollup's value type (Returns) is worked out when the
// type is published, so their values can be sorted and indexed.

// IsComputed reports whether a field's values are computed.
func IsComputed(f Field) bool {
	t, ok := FieldTypeOf(f.Type)
	return ok && t.Computed
}

// MetaOf is the record's own data a metadata field takes its value from:
// _createdAt, _updatedAt or _createdBy; "" for any other field.
func MetaOf(f Field) string {
	switch f.Type {
	case "createdAt":
		return "_createdAt"
	case "updatedAt":
		return "_updatedAt"
	case "createdBy":
		return "_createdBy"
	}
	return ""
}

// ValueField is a field as its values are: a computed field as a field of
// the type its values have (a formula without Returns is left as it is,
// with no order).
func ValueField(f Field) Field {
	v := f
	switch f.Type {
	case "autoNumber":
		v.Type = "number"
	case "createdAt", "updatedAt":
		v.Type = "dateTime"
	case "createdBy":
		v.Type = "user"
	case "formula", "rollup":
		if f.Returns == "" {
			return f
		}
		v.Type = f.Returns
	default:
		return f
	}
	return v
}

// sample is a value of the type a field's values have in a formula, for
// type checking: nil when any value may come.
func sample(f Field) any {
	f = ValueField(f)
	switch f.Type {
	case "number", "decimal":
		return 0.0
	case "money":
		if f.Currency != "" {
			return 0.0
		}
	case "text", "longText", "richText", "enum", "date", "dateTime", "duration", "user", "connection":
		return ""
	case "relation":
		if f.Relation != nil && f.Relation.Many {
			return []any{}
		}
		return ""
	case "bool":
		return false
	case "multiEnum", "tags", "list":
		return []any{}
	}
	return nil
}

// CompileFormula type checks field f's formula against the fields of e
// above it, and says what type its values have: number, text, bool, or
// json when the expression could give anything.
func CompileFormula(e EntityType, f Field) (*vm.Program, string, error) {
	// Fields whose values may be anything (json, or a rollup not yet
	// published) are left out of the typed env and allowed by name
	env, loose := map[string]any{}, map[string]bool{}
	key := f.Formula
	for _, x := range e.Fields {
		if x.ID == f.ID {
			break
		}
		if x.Hidden {
			continue
		}
		if v := sample(x); v != nil {
			env[x.ID] = v
		} else {
			loose[x.ID] = true
		}
		key += fmt.Sprintf("\x00%s:%T", x.ID, sample(x))
	}
	if c, ok := formulas.Load(key); ok {
		c := c.(compiled)
		return c.p, c.returns, c.err
	}
	p, returns, err := compileFormula(f.Formula, env, loose)
	formulas.Store(key, compiled{p, returns, err})
	return p, returns, err
}

// compiled is a formula compiled against the types of the fields it may
// read.
type compiled struct {
	p       *vm.Program
	returns string
	err     error
}

var formulas sync.Map // formula and field types → compiled

func compileFormula(src string, env map[string]any, loose map[string]bool) (*vm.Program, string, error) {
	p, err := expr.Compile(src, expr.Env(env), expr.AllowUndefinedVariables())
	if err != nil {
		return nil, "", err
	}
	names := &names{env: env, loose: loose}
	node := p.Node()
	ast.Walk(&node, names)
	if names.unknown != "" {
		return nil, "", fmt.Errorf("unknown name %s", names.unknown)
	}
	returns := "json"
	if t := p.Node().Type(); t != nil {
		switch t.Kind() {
		case reflect.Float32, reflect.Float64, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			returns = "number"
		case reflect.String:
			returns = "text"
		case reflect.Bool:
			returns = "bool"
		}
	}
	return p, returns, nil
}

// names finds the first name a formula reads that is not a field above it
// nor a variable it declares.
type names struct {
	env      map[string]any
	loose    map[string]bool
	declared map[string]bool
	unknown  string
}

func (n *names) Visit(node *ast.Node) {
	switch x := (*node).(type) {
	case *ast.VariableDeclaratorNode:
		if n.declared == nil {
			n.declared = map[string]bool{}
		}
		n.declared[x.Name] = true
	case *ast.IdentifierNode:
		_, typed := n.env[x.Value]
		if !typed && !n.loose[x.Value] && !n.declared[x.Value] && n.unknown == "" {
			n.unknown = x.Value
		}
	}
}

// Resolve works out the value types of e's formulas and rollups as it is
// published, reading the published types rollups point to with lookup;
// the problems are why it cannot be published.
func Resolve(e *EntityType, lookup func(id string) (EntityType, bool)) []live.Problem {
	var problems []live.Problem
	add := func(path, message, fix string) {
		problems = append(problems, live.Problem{Path: path, Message: message, Fix: fix})
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		p := "fields." + f.ID
		if f.Hidden {
			continue
		}
		switch f.Type {
		case "formula":
			_, returns, err := CompileFormula(*e, *f)
			if err != nil {
				add(p+".formula", fmt.Sprintf("the formula does not type check: %v", err), "use the ids of the fields above it, e.g. \"quantity * price\"")
				continue
			}
			f.Returns = returns
		case "rollup":
			if f.Rollup == nil {
				continue // Check reports it
			}
			if f.Rollup.Aggregate == "count" {
				f.Returns = "number"
				continue
			}
			rel, _, ok := e.FieldByID(f.Rollup.Relation)
			if !ok || rel.Relation == nil {
				continue
			}
			if rel.Relation.Entity == UserEntity {
				add(p+".rollup.aggregate", "a rollup over users can only count them", `use aggregate "count"`)
				continue
			}
			related, ok := lookup(rel.Relation.Entity)
			if rel.Relation.Entity == e.ID {
				related, ok = *e, true
			}
			if !ok {
				add(p+".rollup.relation", fmt.Sprintf("%s is not published yet", rel.Relation.Entity), "publish it first")
				continue
			}
			of, _, found := related.FieldByID(f.Rollup.Field)
			if !found || of.Hidden {
				add(p+".rollup.field", fmt.Sprintf("%s has no field %q", related.Name, f.Rollup.Field), "name one of its fields by id")
				continue
			}
			v := ValueField(of)
			switch f.Rollup.Aggregate {
			case "sum", "avg":
				if v.Type != "number" && v.Type != "decimal" && (v.Type != "money" || v.Currency == "") {
					add(p+".rollup.field", fmt.Sprintf("%s is a %s field, which cannot be summed", of.Name, v.Type), "sum a number, decimal, or money field with a currency")
					continue
				}
				f.Returns, f.Currency = v.Type, v.Currency
				if v.Type != "number" {
					scale := 2
					if v.Scale != nil {
						scale = *v.Scale
					}
					if f.Rollup.Aggregate == "avg" {
						scale += 2
					}
					f.Scale = &scale
				}
			default: // min, max
				if !Sortable(v) {
					add(p+".rollup.field", fmt.Sprintf("%s is a %s field, whose values have no order", of.Name, v.Type), "use a field with an order, or count")
					continue
				}
				f.Returns, f.Scale, f.Currency, f.Options, f.Relation = v.Type, v.Scale, v.Currency, v.Options, v.Relation
			}
		}
	}
	return problems
}

// FormulaEnv is what a formula sees of a record: its fields' values by
// id, decimals and money with a currency as numbers.
func FormulaEnv(e EntityType, record map[string]json.RawMessage) map[string]any {
	env := map[string]any{}
	for _, f := range e.Fields {
		raw := record[f.ID]
		if f.Hidden || IsNull(raw) {
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		vf := ValueField(f)
		if s, ok := v.(string); ok && (vf.Type == "decimal" || vf.Type == "money") {
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				v = n
			}
		}
		if vf.Type == "richText" {
			v = PlainText(raw)
		}
		env[f.ID] = v
	}
	return env
}

// EvalFormula is a formula's value for a record: nil when it has none
// (a value it reads is missing, it fails, or gives something not of its
// type).
func EvalFormula(e EntityType, f Field, record map[string]json.RawMessage) json.RawMessage {
	p, _, err := CompileFormula(e, f)
	if err != nil {
		return nil
	}
	out, err := expr.Run(p, FormulaEnv(e, record))
	if err != nil || out == nil {
		return nil
	}
	switch f.Returns {
	case "number":
		n, ok := toFloat(out)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil
		}
		out = n
	case "text":
		if _, ok := out.(string); !ok {
			return nil
		}
	case "bool":
		if _, ok := out.(bool); !ok {
			return nil
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

func toFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch {
	case rv.CanFloat():
		return rv.Float(), true
	case rv.CanInt():
		return float64(rv.Int()), true
	case rv.CanUint():
		return float64(rv.Uint()), true
	}
	return 0, false
}

// SearchableType reports whether a field's values can be searched as text.
func SearchableType(f Field) bool {
	f = ValueField(f)
	return slices.Contains([]string{"text", "longText", "richText", "enum", "multiEnum", "tags"}, f.Type)
}
