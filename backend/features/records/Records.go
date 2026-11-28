// Package records is the data model of applications (documentation/Records.md):
// entity types, defined by entity.* commands on the def stream, and, from
// Forge's part 3b, their records. This package holds the definitions'
// types, the field types and the checks every definition passes;
// records/entities holds the commands, projections and queries.
package records

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the records feature's logging and error builder.
var Log = logging.In("records")

// EntityType is a schema for records, such as Task or Customer. Records
// keep their values by field id, so renaming a field never touches them.
type EntityType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"` // for a model: what a record of it is
	// Owner is what it belongs to: "app:<appId>", or "tenant" for a type
	// apps share, such as Customer.
	Owner      string  `json:"owner,omitempty"`
	TitleField string  `json:"titleField,omitempty"` // the field shown as a record's title
	Fields     []Field `json:"fields"`
	Indexes    []Index `json:"indexes"` // declared here, built from Forge's part 3d
	Access     Access  `json:"access"`  // declared here, enforced from Forge's part 3g
	// Storage is how its records are kept beyond the stream and the KV
	// projection (Forge's part 7k).
	Storage Storage `json:"storage,omitzero"`
	Version int     `json:"version"` // the published version it is; 0 before the first
	// Hidden is a deleted entity type: its records are kept, and
	// entity.restore brings it back with them.
	Hidden bool `json:"hidden,omitempty"`
	// UserDefined is a type a running app made after install (forge
	// 35c-1, entity.createUserDefined): a Docs database's properties. It
	// is owned by the app but part of no app version, so an upgrade leaves
	// it alone and app.describe and app.export do not list it; Parent is
	// the record that holds it, on whose path record.define decides who
	// may change it.
	UserDefined bool    `json:"userDefined,omitempty"`
	Parent      *Parent `json:"parent,omitempty"`
}

// Parent is the record holding a user-defined entity type (forge 35c-1),
// such as a Docs Database record.
type Parent struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

// Field is one field of an entity type. Which of the type-specific parts
// it uses depends on its Type (FieldTypes).
type Field struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Required    bool            `json:"required,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	// Hidden is a removed field: its values are kept until a purge, and
	// restoring it shows them again.
	Hidden   bool            `json:"hidden,omitempty"`
	Options  []Option        `json:"options,omitempty"`  // enum, multiEnum
	Relation *Relation       `json:"relation,omitempty"` // relation
	Scale    *int            `json:"scale,omitempty"`    // decimal, money: digits after the point
	Currency string          `json:"currency,omitempty"` // money: an ISO 4217 code; any when empty
	Contract string          `json:"contract,omitempty"` // connection: only connections of types implementing it (id@version)
	Kind     string          `json:"kind,omitempty"`     // principal: the kind of principal its records define (agent, the default)
	Scope    string          `json:"scope,omitempty"`    // rank: the field whose value it orders within (a task's status); the whole type when empty
	Items    *Field          `json:"items,omitempty"`    // list: what each item is (its id and name are not used)
	Fields   []Field         `json:"fields,omitempty"`   // object: its nested fields
	Schema   json.RawMessage `json:"schema,omitempty"`   // json: the JSON Schema its values follow
	Formula  string          `json:"formula,omitempty"`  // formula: an expression over the record's fields by id
	Rollup   *Rollup         `json:"rollup,omitempty"`   // rollup
	// Returns is the type of a formula's or rollup's values (number, text,
	// bool, decimal …), worked out when the entity type is published
	// (Resolve); Scale, Currency, Options and Relation then describe them
	// as they would a field of that type.
	Returns string `json:"returns,omitempty"`
	// Searchable puts the field's text in the records' full-text and
	// vector search (Records.md › Built in slice 24).
	Searchable bool `json:"searchable,omitempty"`
}

// Option is one choice of an enum or multiEnum field.
type Option struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Relation is where a relation field points.
type Relation struct {
	Entity  string `json:"entity"`            // the entity type; "user" is the tenant's users
	Many    bool   `json:"many,omitempty"`    // a list of records rather than one
	Inverse string `json:"inverse,omitempty"` // the field on Entity that points back
}

// Rollup is an aggregate over the records a relation field points to.
type Rollup struct {
	Relation  string `json:"relation"`        // the relation field's id
	Field     string `json:"field,omitempty"` // the related type's field; not needed to count
	Aggregate string `json:"aggregate"`       // Aggregates
}

// Aggregates are what a rollup can compute.
var Aggregates = []string{"count", "sum", "min", "max", "avg"}

// Index is a declared index over fields, in order.
type Index struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
	Unique bool     `json:"unique,omitempty"`
}

// Access is an entity type's row rules: an expression over the record and
// the principal per action (e.g. `record.owner == principal.user`), empty
// for no rule.
type Access struct {
	Read   string `json:"read,omitempty"`
	Create string `json:"create,omitempty"`
	Update string `json:"update,omitempty"`
	Delete string `json:"delete,omitempty"`
}

// Storage is how an entity type's records are kept for queries: the KV
// projection alone (the default), or also a table in the tenant database
// that the query planner reads (Records.md › Built in slice 52).
type Storage struct {
	// Projection is "" for the KV projection alone, or ProjectionSQL.
	Projection string `json:"projection,omitempty"`
}

// ProjectionSQL projects an entity type's records into a table of the
// tenant database too: a large table, listed, counted and sorted by SQL.
const ProjectionSQL = "sql"

// Projections are the values Storage.Projection can have.
var Projections = []string{"", ProjectionSQL}

// SQL reports whether the entity type's records are projected into a table.
func (e EntityType) SQL() bool { return e.Storage.Projection == ProjectionSQL }

// UserEntity is the built-in entity type of the tenant's users, which
// relation fields may point to.
const UserEntity = "user"

// FieldByID is a field by id, hidden or not.
func (e EntityType) FieldByID(id string) (Field, int, bool) {
	for i, f := range e.Fields {
		if f.ID == id {
			return f, i, true
		}
	}
	return Field{}, -1, false
}

// Visible is the fields that are not hidden.
func (e EntityType) Visible() []Field {
	out := []Field{}
	for _, f := range e.Fields {
		if !f.Hidden {
			out = append(out, f)
		}
	}
	return out
}
