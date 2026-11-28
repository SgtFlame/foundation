package chronicle

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"gorm.io/gorm"
)

// Scopes of entries and entry types (forge 35f-6): a tenant's, or a
// project's, "project:<page>", read by those who may read the page's
// subtree (ProjectAccess).
const (
	ScopeTenant  = "tenant"
	ScopeProject = "project:"
)

// EntryRelation is the relation entity a field names to point at another
// entry ({"type": "relation", "relation": {"entity": "chronicle"}}).
const EntryRelation = "chronicle"

// TypeDef is an entry type a tenant or a project defines (forge 35f-6,
// Chronicle.md › Built in forge 35f-6), kept in the tenant database. Its
// fields are the records' field catalog, so a story bible's Character has
// an age, a home (a relation to a Place entry) and a status.
type TypeDef struct {
	db.Model
	db.Tenanted
	TypeID      string `gorm:"index"` // what entries' Type names; unique within its scope
	Scope       string `gorm:"index"` // ScopeTenant or ScopeProject+<page>
	Title       string
	Icon        string
	Description string
	Fields      string // JSON []records.Field
	ParentTypes string // JSON []string
	Starter     string // the starter set it was copied from, if any
}

// EntryType is the type a definition describes.
func (d TypeDef) EntryType() EntryType {
	var fields []records.Field
	var parents []string
	_ = json.Unmarshal([]byte(d.Fields), &fields)
	_ = json.Unmarshal([]byte(d.ParentTypes), &parents)
	return EntryType{ID: d.TypeID, Title: d.Title, Icon: d.Icon, Description: d.Description, Fields: FormSchema(fields),
		RecordFields: fields, ParentTypes: parents, Scope: d.Scope, Custom: true}
}

// ValidScope reports whether s is a scope a type or an entry may have.
func ValidScope(s string) bool {
	if s == ScopeTenant {
		return true
	}
	page, ok := strings.CutPrefix(s, ScopeProject)
	return ok && live.ValidToken(page)
}

// CheckType checks a definition's id, title and fields, normalizing its
// fields; the problems name paths like fields.<id>.type.
func CheckType(t *EntryType) []live.Problem {
	var problems []live.Problem
	add := func(path, msg string) { problems = append(problems, live.Problem{Path: path, Message: msg}) }
	if !live.ValidToken(t.ID) || strings.HasPrefix(t.ID, "_") {
		add("id", fmt.Sprintf("%q is not a valid type id", t.ID))
	}
	if _, builtin := TypeOf(t.ID); builtin {
		add("id", fmt.Sprintf("%q is a built-in type", t.ID))
	}
	if strings.TrimSpace(t.Title) == "" {
		add("title", "the type needs a title")
	}
	if !ValidScope(t.Scope) {
		add("scope", fmt.Sprintf("%q is not tenant or project:<page>", t.Scope))
	}
	for i := range t.RecordFields {
		f := &t.RecordFields[i]
		if f.ID == "" {
			f.ID = fieldID(f.Name)
		}
		records.Normalize(f)
		if t, _ := records.FieldTypeOf(f.Type); t.Computed {
			add("fields."+f.ID+".type", "an entry's fields keep values: "+f.Type+" is computed")
		}
	}
	e := records.EntityType{ID: "chronicle_" + t.ID, Name: t.Title, Fields: t.RecordFields, Indexes: []records.Index{}}
	for _, p := range records.Check(e, func(id string) bool { return id == EntryRelation }) {
		if p.Path == "titleField" {
			continue
		}
		problems = append(problems, p)
	}
	t.Fields = FormSchema(t.RecordFields)
	return problems
}

// fieldID is a field id made from its name: "Home place" is home_place.
func fieldID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "_"):
			b.WriteRune('_')
		}
	}
	if s := strings.TrimSuffix(b.String(), "_"); s != "" {
		return s
	}
	return "field"
}

// CheckFields checks and normalizes an entry's field values against its
// type's record fields (records.CheckValue), dropping fields the type does
// not have (kept from before a type change).
func CheckFields(t EntryType, values map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, f := range t.RecordFields {
		raw, ok := values[f.ID]
		if !ok || records.IsNull(raw) {
			if f.Required {
				return nil, fmt.Errorf("%s needs a value", f.Name)
			}
			continue
		}
		v, err := records.CheckValue(f, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", f.Name, err)
		}
		if v != nil {
			out[f.ID] = v
		}
	}
	return out, nil
}

// FormSchema is a type's record fields in the configuration schema
// language the entry editor's form renders: text and choices as strings
// and enums, numbers as numbers, a relation to another entry as its id.
func FormSchema(fields []records.Field) json.RawMessage {
	props := map[string]any{}
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		p := map[string]any{"title": f.Name, "description": f.Description}
		switch f.Type {
		case "number", "integer", "decimal", "money", "percent", "rating", "duration":
			p["type"] = "number"
		case "bool":
			p["type"] = "boolean"
		case "enum":
			var options []map[string]string
			for _, o := range f.Options {
				options = append(options, map[string]string{"value": o.ID, "label": o.Name})
			}
			p["type"], p["options"] = "enum", options
		case "relation":
			p["type"] = "string"
			if f.Relation != nil && f.Relation.Entity == EntryRelation {
				p["description"] = strings.TrimSpace(f.Description + " (an entry's id)")
				p["entry"], p["many"] = true, f.Relation.Many
			}
		default:
			p["type"] = "string"
		}
		props[f.ID] = p
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props})
	return raw
}

// Facts is an entry's typed fields and links as text (forge 35f-6), kept
// on the entry (Entry.Facts) so the entries table finds "Wenlan's sister":
// a line per field with a value, by the field's name, an option by its
// name and a related entry by its title, then a line per typed link.
func Facts(t EntryType, fields map[string]json.RawMessage, links []EntryLink, self uint64, titles map[uint64]string) string {
	var lines []string
	for _, f := range t.RecordFields {
		raw, ok := fields[f.ID]
		if !ok || f.Hidden {
			continue
		}
		if s := factValue(f, raw, titles); s != "" {
			lines = append(lines, f.Name+": "+s)
		}
	}
	var linked []string
	for _, l := range links {
		if l.Kind == "" || l.Kind == LinkRelated {
			continue
		}
		switch self {
		case l.FromID:
			if title := titles[l.ToID]; title != "" {
				linked = append(linked, l.Kind+" "+title)
			}
		case l.ToID:
			if title := titles[l.FromID]; title != "" {
				linked = append(linked, title+" is "+l.Kind+" this")
			}
		}
	}
	sort.Strings(linked)
	return strings.Join(append(lines, linked...), "\n")
}

func factValue(f records.Field, raw json.RawMessage, titles map[uint64]string) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	one := func(x any) string {
		s := fmt.Sprint(x)
		if f.Type == "enum" || f.Type == "multiEnum" {
			if i := slices.IndexFunc(f.Options, func(o records.Option) bool { return o.ID == s }); i >= 0 {
				return f.Options[i].Name
			}
		}
		if f.Type == "relation" && f.Relation != nil && f.Relation.Entity == EntryRelation {
			var id uint64
			if _, err := fmt.Sscan(s, &id); err == nil && titles[id] != "" {
				return titles[id]
			}
		}
		return s
	}
	if list, ok := v.([]any); ok {
		parts := make([]string, 0, len(list))
		for _, x := range list {
			parts = append(parts, one(x))
		}
		return strings.Join(parts, ", ")
	}
	if v == nil {
		return ""
	}
	return one(v)
}

// Relations named in a field (relation to an entry) are what Facts
// resolves; RelatedIDs gives them.
func RelatedIDs(t EntryType, fields map[string]json.RawMessage) []uint64 {
	var ids []uint64
	for _, f := range t.RecordFields {
		if f.Type != "relation" || f.Relation == nil || f.Relation.Entity != EntryRelation {
			continue
		}
		var one string
		var many []string
		raw := fields[f.ID]
		if json.Unmarshal(raw, &one) == nil {
			many = []string{one}
		} else {
			_ = json.Unmarshal(raw, &many)
		}
		for _, s := range many {
			var id uint64
			if _, err := fmt.Sscan(s, &id); err == nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// TypesIn are the built-in types, then the custom types of the tenant and
// of the project scopes given (ReadableProjects), by scope and title.
func TypesIn(gdb *gorm.DB, tenant uint64, projects []string) ([]EntryType, error) {
	out := slices.Clone(Types)
	var defs []TypeDef
	err := gdb.Scopes(db.InTenant(tenant)).Where("scope IN ?", append([]string{ScopeTenant}, projects...)).Order("scope, title, id").Find(&defs).Error
	for _, d := range defs {
		out = append(out, d.EntryType())
	}
	return out, err
}

// TypeFor is an entry's type: a built-in one, else its project's (when
// the entry has a project scope), else its tenant's.
func TypeFor(gdb *gorm.DB, tenant uint64, id, scope string) (EntryType, bool, error) {
	if t, ok := TypeOf(id); ok {
		return t, true, nil
	}
	scopes := []string{ScopeTenant}
	if strings.HasPrefix(scope, ScopeProject) {
		scopes = []string{scope, ScopeTenant}
	}
	var defs []TypeDef
	if err := gdb.Scopes(db.InTenant(tenant)).Where("type_id = ? AND scope IN ?", id, scopes).Find(&defs).Error; err != nil {
		return EntryType{}, false, err
	}
	for _, s := range scopes {
		for _, d := range defs {
			if d.Scope == s {
				return d.EntryType(), true, nil
			}
		}
	}
	return EntryType{}, false, nil
}

// RefreshFacts writes e's Facts again from its fields and typed links,
// naming related entries its owner may see (their own, the tenant's
// shared ones, its project's). It reports whether they changed.
func RefreshFacts(gdb *gorm.DB, e *Entry) (bool, error) {
	t, _, err := TypeFor(gdb, e.TenantID, e.Type, e.Scope)
	if err != nil {
		return false, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(e.Fields), &fields)
	var links []EntryLink
	if err := gdb.Scopes(db.InTenant(e.TenantID)).Where("(from_id = ? OR to_id = ?) AND kind <> ? AND kind <> ''", e.ID, e.ID, LinkRelated).
		Order("id").Find(&links).Error; err != nil {
		return false, err
	}
	ids := RelatedIDs(t, fields)
	for _, l := range links {
		ids = append(ids, l.FromID, l.ToID)
	}
	titles := map[uint64]string{}
	if len(ids) > 0 {
		var related []Entry
		q := gdb.Scopes(db.InTenant(e.TenantID)).Select("id", "title").Where("id IN ?", ids)
		if e.Scope != "" {
			q = q.Where("owner_id = ? OR shared <> '' OR scope = ?", e.OwnerID, e.Scope)
		} else {
			q = q.Where("owner_id = ? OR shared <> ''", e.OwnerID)
		}
		if err := q.Find(&related).Error; err != nil {
			return false, err
		}
		for _, r := range related {
			titles[r.ID] = r.Title
		}
	}
	facts := Facts(t, fields, links, e.ID, titles)
	if facts == e.Facts {
		return false, nil
	}
	e.Facts = facts
	return true, gdb.Model(&Entry{}).Where("id = ?", e.ID).UpdateColumn("facts", facts).Error
}

// Referrers are the entries whose facts name e: linked to it by a typed
// link, or naming its id in a field.
func Referrers(gdb *gorm.DB, e Entry) ([]Entry, error) {
	var linked []uint64
	var links []EntryLink
	if err := gdb.Scopes(db.InTenant(e.TenantID)).Where("(from_id = ? OR to_id = ?) AND kind <> ? AND kind <> ''", e.ID, e.ID, LinkRelated).Find(&links).Error; err != nil {
		return nil, err
	}
	for _, l := range links {
		linked = append(linked, l.FromID, l.ToID)
	}
	var out []Entry
	needle := "%" + strconv.Quote(strconv.FormatUint(e.ID, 10)) + "%"
	err := gdb.Scopes(db.InTenant(e.TenantID)).Where("id <> ? AND (id IN ? OR fields LIKE ?)", e.ID, append(linked, 0), needle).Find(&out).Error
	return out, err
}
