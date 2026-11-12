package records

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/live"
)

var currency = regexp.MustCompile(`^[A-Z]{3}$`)

// Normalize completes a field's type-specific parts: the scale of a
// decimal or money field, and the same for its items and nested fields.
func Normalize(f *Field) {
	if (f.Type == "decimal" || f.Type == "money") && f.Scale == nil {
		two := 2
		f.Scale = &two
	}
	if f.Items != nil {
		Normalize(f.Items)
	}
	for i := range f.Fields {
		Normalize(&f.Fields[i])
	}
}

// Check is every problem with a definition. known reports whether an
// entity type exists in the tenant, for relations; the type itself and
// UserEntity always do. Paths are within the definition, such as
// "fields.due.type".
func Check(e EntityType, known func(id string) bool) []live.Problem {
	var problems []live.Problem
	add := func(path, message, fix string) {
		problems = append(problems, live.Problem{Path: path, Message: message, Fix: fix})
	}
	if strings.TrimSpace(e.Name) == "" {
		add("name", "an entity type needs a name", "")
	}
	exists := func(id string) bool { return id == e.ID || id == UserEntity || (known != nil && known(id)) }
	for path, message := range checkAccess(e.Access) {
		add(path, message, `use record.<field id>, record._createdBy and principal.user, e.g. "record.assignee == principal.user"`)
	}
	problems = append(problems, checkFields(e.Fields, "fields", exists, &e)...)

	visible := map[string]Field{}
	for _, f := range e.Visible() {
		visible[f.ID] = f
	}
	if e.TitleField != "" {
		if _, ok := visible[e.TitleField]; !ok {
			add("titleField", fmt.Sprintf("there is no field %q to show as the title", e.TitleField), "set titleField to one of "+fieldList(e))
		}
	}
	ids := map[string]bool{}
	for i, x := range e.Indexes {
		path := fmt.Sprintf("indexes.%d", i)
		if !live.ValidToken(x.ID) || strings.HasPrefix(x.ID, "_") || ids[x.ID] {
			add(path+".id", fmt.Sprintf("%q is not a new, valid index id", x.ID), "use letters, digits, '-' and '_'")
		}
		ids[x.ID] = true
		if len(x.Fields) == 0 {
			add(path+".fields", "an index needs at least one field", "")
		}
		lists := 0
		for j, id := range x.Fields {
			f, ok := visible[id]
			switch {
			case !ok:
				add(fmt.Sprintf("%s.fields.%d", path, j), fmt.Sprintf("index %s names %q, which is not a field", x.ID, id), "index one of "+fieldList(e)+", or remove the index with entity.removeIndex")
			case !Indexable(f):
				add(fmt.Sprintf("%s.fields.%d", path, j), fmt.Sprintf("index %s names %s, a %s field, whose values have no order", x.ID, id, f.Type), "index text, number, decimal, money, bool, date, dateTime, duration, enum, user or relation fields, or lists of them")
			default:
				if _, many := ItemOf(f); many {
					if lists++; lists == 2 {
						add(fmt.Sprintf("%s.fields.%d", path, j), fmt.Sprintf("index %s has more than one list-like field", x.ID), "index one list-like field at most; it is indexed once per item")
					}
				}
			}
		}
	}
	return problems
}

// checkFields checks one level of fields; e is the entity type at the top
// level, nil in nested objects.
func checkFields(fields []Field, path string, exists func(string) bool, e *EntityType) []live.Problem {
	var problems []live.Problem
	add := func(path, message, fix string) {
		problems = append(problems, live.Problem{Path: path, Message: message, Fix: fix})
	}
	ids, names := map[string]bool{}, map[string]string{}
	for _, f := range fields {
		p := path + "." + f.ID
		if !live.ValidToken(f.ID) || ids[f.ID] || strings.HasPrefix(f.ID, "_") {
			add(p+".id", fmt.Sprintf("%q is not a new, valid field id", f.ID), "use letters, digits, '-' and '_', not starting with '_' (records keep their own data there)")
		}
		ids[f.ID] = true
		name := strings.ToLower(strings.TrimSpace(f.Name))
		switch {
		case name == "":
			add(p+".name", "a field needs a name", "")
		case f.Hidden:
		case names[name] != "":
			add(p+".name", fmt.Sprintf("there is already a field named %q (%s)", f.Name, names[name]), "choose another name, or rename the other field first")
		default:
			names[name] = f.ID
		}
		typeProblems := checkType(f, p, exists, e)
		problems = append(problems, typeProblems...)
		if f.Searchable && !SearchableType(f) {
			add(p+".searchable", fmt.Sprintf("%s is a %s field, which is not searched as text", f.Name, f.Type), "mark text, longText, richText, enum, multiEnum or tags fields searchable, or formulas giving text")
		}
		if len(typeProblems) == 0 && !IsNull(f.Default) {
			if _, err := CheckValue(f, f.Default); err != nil {
				add(p+".default", "the default "+err.Error(), "give a default of the field's type, or null for none")
			}
		}
	}
	return problems
}

func checkType(f Field, p string, exists func(string) bool, e *EntityType) []live.Problem {
	var problems []live.Problem
	add := func(path, message, fix string) {
		problems = append(problems, live.Problem{Path: path, Message: message, Fix: fix})
	}
	t, ok := FieldTypeOf(f.Type)
	if !ok {
		add(p+".type", fmt.Sprintf("there is no field type %q", f.Type), "list the field types with entity.listFieldTypes")
		return problems
	}
	switch t.ID {
	case "enum", "multiEnum":
		if len(f.Options) == 0 {
			add(p+".options", "a choice field needs options", `give options [{id, name, color}], e.g. [{"id":"low","name":"Low"}]`)
		}
		ids := map[string]bool{}
		for i, o := range f.Options {
			if !live.ValidToken(o.ID) || ids[o.ID] {
				add(fmt.Sprintf("%s.options.%d.id", p, i), fmt.Sprintf("%q is not a new, valid option id", o.ID), "use letters, digits, '-' and '_'")
			}
			ids[o.ID] = true
			if strings.TrimSpace(o.Name) == "" {
				add(fmt.Sprintf("%s.options.%d.name", p, i), "an option needs a name", "")
			}
		}
	case "relation":
		switch {
		case f.Relation == nil || f.Relation.Entity == "":
			add(p+".relation", "a relation needs the entity type it points to", `give relation {entity: "<entity type id>"}`)
		case !exists(f.Relation.Entity):
			add(p+".relation.entity", fmt.Sprintf("there is no entity type %q", f.Relation.Entity), "list the entity types with entity.list, or create it first with entity.create")
		case f.Relation.Inverse != "" && !live.ValidToken(f.Relation.Inverse):
			add(p+".relation.inverse", fmt.Sprintf("%q is not a valid field id", f.Relation.Inverse), "")
		}
	case "decimal", "money":
		if f.Scale != nil && (*f.Scale < 0 || *f.Scale > 18) {
			add(p+".scale", "a scale is from 0 to 18 digits", "")
		}
		if f.Currency != "" && (t.ID != "money" || !currency.MatchString(f.Currency)) {
			add(p+".currency", fmt.Sprintf("%q is not an ISO 4217 currency code of a money field", f.Currency), `use three capital letters, e.g. "USD"`)
		}
	case "connection":
		if f.Contract != "" {
			if id, v, ok := strings.Cut(f.Contract, "@"); !ok || id == "" || v == "" || strings.Trim(v, "0123456789") != "" {
				add(p+".contract", fmt.Sprintf("%q is not a contract reference", f.Contract), "write id@version, such as mail.mailbox@1 (connectors.contracts)")
			}
		}
	case "json":
		if f.Schema != nil {
			var s map[string]any
			if json.Unmarshal(f.Schema, &s) != nil {
				add(p+".schema", "a schema is a JSON Schema object", "")
			}
		}
	case "list":
		if f.Items == nil {
			add(p+".items", "a list needs the type of its items", `give items {type: "text"}`)
			break
		}
		item := *f.Items
		if it, ok := FieldTypeOf(item.Type); ok && it.Computed {
			add(p+".items.type", "a list cannot hold computed values", "")
			break
		}
		if item.ID == "" {
			item.ID = "items"
		}
		if item.Name == "" {
			item.Name = f.Name
		}
		problems = append(problems, checkType(item, p+".items", exists, nil)...)
	case "object":
		if len(f.Fields) == 0 {
			add(p+".fields", "an object needs fields", "")
		}
		problems = append(problems, checkFields(f.Fields, p+".fields", exists, nil)...)
	case "formula":
		switch {
		case e == nil:
			add(p+".type", "a formula belongs at the top level of an entity type", "")
		case strings.TrimSpace(f.Formula) == "":
			add(p+".formula", "a formula field needs its expression", `give formula, e.g. "quantity * price"`)
		default:
			if _, _, err := CompileFormula(*e, f); err != nil {
				add(p+".formula", fmt.Sprintf("the formula does not type check: %v", err), "use the ids of the fields above it (a formula reads only those), e.g. \"quantity * price\"")
			}
		}
	case "autoNumber", "createdAt", "updatedAt", "createdBy":
		if e == nil {
			add(p+".type", fmt.Sprintf("a %s field belongs at the top level of an entity type", f.Type), "")
		}
	case "rollup":
		if e == nil {
			add(p+".type", "a rollup belongs at the top level of an entity type", "")
			break
		}
		if f.Rollup == nil {
			add(p+".rollup", "a rollup needs its relation and aggregate", `give rollup {relation: "<relation field id>", aggregate: "count"}`)
			break
		}
		if r, _, ok := e.FieldByID(f.Rollup.Relation); !ok || r.Hidden || r.Type != "relation" {
			add(p+".rollup.relation", fmt.Sprintf("%q is not a relation field of this entity type", f.Rollup.Relation), "")
		}
		if !slices.Contains(Aggregates, f.Rollup.Aggregate) {
			add(p+".rollup.aggregate", fmt.Sprintf("there is no aggregate %q", f.Rollup.Aggregate), "use one of "+strings.Join(Aggregates, ", "))
		} else if f.Rollup.Aggregate != "count" && f.Rollup.Field == "" {
			add(p+".rollup.field", "a rollup other than count needs the related field it aggregates", "")
		}
	}
	return problems
}

func fieldList(e EntityType) string {
	var ids []string
	for _, f := range e.Visible() {
		ids = append(ids, f.ID)
	}
	if len(ids) == 0 {
		return "its fields (it has none yet)"
	}
	return strings.Join(ids, ", ")
}
