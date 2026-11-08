package records

import "encoding/json"

// FieldType is one type a field can have.
type FieldType struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Group       string   `json:"group"`
	Description string   `json:"description"` // for a model: what it holds and what it needs
	Computed    bool     `json:"computed,omitempty"`
	Tags        []string `json:"tags"`
	// Examples are fields of the type as entity.addField takes them (forge
	// 06d); a formula's names quantity and price, a rollup's lines, fields
	// of the entity type it is in.
	Examples []json.RawMessage `json:"examples"`
}

// Field type groups.
const (
	GroupBasic      = "basic"
	GroupChoices    = "choices"
	GroupReferences = "references"
	GroupStructured = "structured"
	GroupComputed   = "computed"
)

// FieldTypes are every field type. Computed ones are never set by a
// command: the records' decisions write their values (Records.md › Built
// in slice 24).
var FieldTypes = []FieldType{
	{ID: "text", Title: "Text", Group: GroupBasic, Description: "One line of text",
		Tags: []string{"string", "short"}, Examples: example(`{"name":"Title","type":"text","required":true}`)},
	{ID: "longText", Title: "Long text", Group: GroupBasic, Description: "Plain text of several lines",
		Tags: []string{"string", "notes", "multiline"}, Examples: example(`{"name":"Notes","type":"longText"}`)},
	{ID: "richText", Title: "Rich text", Group: GroupBasic, Description: "Formatted text: a ProseMirror document {type: \"doc\", content: [blocks]}, saved whole by record.setRichText",
		Tags: []string{"formatted", "document", "prosemirror"}, Examples: example(`{"name":"Body","type":"richText","searchable":true}`)},
	{ID: "number", Title: "Number", Group: GroupBasic, Description: "A floating-point number",
		Tags: []string{"numeric", "float"}, Examples: example(`{"name":"Quantity","type":"number","default":1}`)},
	{ID: "decimal", Title: "Decimal", Group: GroupBasic, Description: "An exact decimal number with scale digits after the point (2 when left out)",
		Tags: []string{"numeric", "exact"}, Examples: example(`{"name":"Rate","type":"decimal","scale":4}`)},
	{ID: "money", Title: "Money", Group: GroupBasic, Description: "An exact amount with scale (2 when left out) and an ISO 4217 currency; any currency when it has none",
		Tags: []string{"numeric", "currency", "price"}, Examples: example(`{"name":"Price","type":"money","scale":2,"currency":"EUR"}`)},
	{ID: "bool", Title: "Yes/no", Group: GroupBasic, Description: "True or false",
		Tags: []string{"boolean", "checkbox", "flag"}, Examples: example(`{"name":"Done","type":"bool","default":false}`)},
	{ID: "date", Title: "Date", Group: GroupBasic, Description: "A calendar date, YYYY-MM-DD",
		Tags: []string{"calendar", "day"}, Examples: example(`{"name":"Due","type":"date"}`)},
	{ID: "dateTime", Title: "Date and time", Group: GroupBasic, Description: "An instant, RFC 3339",
		Tags: []string{"timestamp", "instant"}, Examples: example(`{"name":"Starts at","type":"dateTime"}`)},
	{ID: "duration", Title: "Duration", Group: GroupBasic, Description: "A length of time",
		Tags: []string{"time", "length"}, Examples: example(`{"name":"Estimate","type":"duration"}`)},
	{ID: "enum", Title: "Choice", Group: GroupChoices, Description: "One of its options {id, name, color}",
		Tags: []string{"select", "status", "options"}, Examples: example(`{"name":"Priority","type":"enum","options":[{"id":"low","name":"Low"},{"id":"high","name":"High","color":"red"}]}`)},
	{ID: "multiEnum", Title: "Choices", Group: GroupChoices, Description: "Any of its options {id, name, color}",
		Tags: []string{"multi-select", "options"}, Examples: example(`{"name":"Channels","type":"multiEnum","options":[{"id":"email","name":"Email"},{"id":"phone","name":"Phone"}]}`)},
	{ID: "tags", Title: "Tags", Group: GroupChoices, Description: "Free-form tags",
		Tags: []string{"labels", "list"}, Examples: example(`{"name":"Tags","type":"tags"}`)},
	{ID: "relation", Title: "Relation", Group: GroupReferences, Description: "A record (or with many, records) of another entity type: relation {entity, many, inverse}; entity \"user\" is the tenant's users",
		Tags: []string{"reference", "link", "foreign key"}, Examples: example(`{"name":"Customer","type":"relation","relation":{"entity":"customer"}}`)},
	{ID: "user", Title: "User", Group: GroupReferences, Description: "One of the tenant's users",
		Tags: []string{"person", "assignee", "owner"}, Examples: example(`{"name":"Assignee","type":"user"}`)},
	{ID: "file", Title: "File", Group: GroupReferences, Description: "An uploaded file {id, name, size, contentType}: upload it with POST /records/files/upload (or the Upload file node), then set the reference it gives",
		Tags: []string{"upload", "attachment"}, Examples: example(`{"name":"Attachment","type":"file"}`)},
	{ID: "volumeFile", Title: "Volume file", Group: GroupReferences, Description: "A file in one of the tenant's Volumes {volumeId, path}, path relative to the volume",
		Tags: []string{"file", "volume", "path"}, Examples: example(`{"name":"Document","type":"volumeFile"}`)},
	{ID: "json", Title: "JSON", Group: GroupStructured, Description: "Any JSON value, following schema when it has one",
		Tags: []string{"structured", "any"}, Examples: example(`{"name":"Metadata","type":"json","schema":{"type":"object"}}`)},
	{ID: "list", Title: "List", Group: GroupStructured, Description: "A list of values, each as items describes ({type, …})",
		Tags: []string{"array", "repeated"}, Examples: example(`{"name":"Scores","type":"list","items":{"type":"number"}}`)},
	{ID: "object", Title: "Object", Group: GroupStructured, Description: "Nested fields",
		Tags: []string{"nested", "group"}, Examples: example(`{"name":"Address","type":"object","fields":[{"id":"street","name":"Street","type":"text"},{"id":"city","name":"City","type":"text"}]}`)},
	{ID: "formula", Title: "Formula", Group: GroupComputed, Computed: true, Description: "Computed from the record by formula, an expr-lang expression over its fields by id, e.g. \"quantity * price\"",
		Tags: []string{"computed", "expression", "calculated"}, Examples: example(`{"name":"Total","type":"formula","formula":"quantity * price"}`)},
	{ID: "rollup", Title: "Rollup", Group: GroupComputed, Computed: true, Description: "An aggregate over the records a relation field points to: rollup {relation, field, aggregate: count|sum|min|max|avg}, kept current as they change",
		Tags: []string{"computed", "aggregate", "sum", "count"}, Examples: example(`{"name":"Line count","type":"rollup","rollup":{"relation":"lines","aggregate":"count"}}`)},
	{ID: "autoNumber", Title: "Auto number", Group: GroupComputed, Computed: true, Description: "A number given to each new record, counting up from 1 with no gaps",
		Tags: []string{"computed", "sequence", "counter"}, Examples: example(`{"name":"Number","type":"autoNumber"}`)},
	{ID: "createdAt", Title: "Created at", Group: GroupComputed, Computed: true, Description: "When the record was made",
		Tags: []string{"computed", "timestamp", "audit"}, Examples: example(`{"name":"Created","type":"createdAt"}`)},
	{ID: "updatedAt", Title: "Updated at", Group: GroupComputed, Computed: true, Description: "When the record last changed",
		Tags: []string{"computed", "timestamp", "audit"}, Examples: example(`{"name":"Updated","type":"updatedAt"}`)},
	{ID: "createdBy", Title: "Created by", Group: GroupComputed, Computed: true, Description: "The user who made the record",
		Tags: []string{"computed", "user", "audit"}, Examples: example(`{"name":"Creator","type":"createdBy"}`)},
}

func example(field string) []json.RawMessage { return []json.RawMessage{json.RawMessage(field)} }

// FieldTypeOf finds a field type.
func FieldTypeOf(id string) (FieldType, bool) {
	for _, t := range FieldTypes {
		if t.ID == id {
			return t, true
		}
	}
	return FieldType{}, false
}
