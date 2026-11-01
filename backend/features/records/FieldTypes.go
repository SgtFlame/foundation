package records

// FieldType is one type a field can have.
type FieldType struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Description string `json:"description"` // for a model: what it holds and what it needs
	Computed    bool   `json:"computed,omitempty"`
}

// Field type groups.
const (
	GroupBasic      = "basic"
	GroupChoices    = "choices"
	GroupReferences = "references"
	GroupStructured = "structured"
	GroupComputed   = "computed"
)

// FieldTypes are every field type. A definition accepts all of them; which
// values are checked is up to the records (Forge's parts 3b and 3j).
var FieldTypes = []FieldType{
	{ID: "text", Group: GroupBasic, Description: "One line of text"},
	{ID: "longText", Group: GroupBasic, Description: "Plain text of several lines"},
	{ID: "richText", Group: GroupBasic, Description: "Formatted text, as block JSON"},
	{ID: "number", Group: GroupBasic, Description: "A floating-point number"},
	{ID: "decimal", Group: GroupBasic, Description: "An exact decimal number with scale digits after the point (2 when left out)"},
	{ID: "money", Group: GroupBasic, Description: "An exact amount with scale (2 when left out) and an ISO 4217 currency; any currency when it has none"},
	{ID: "bool", Group: GroupBasic, Description: "True or false"},
	{ID: "date", Group: GroupBasic, Description: "A calendar date, YYYY-MM-DD"},
	{ID: "dateTime", Group: GroupBasic, Description: "An instant, RFC 3339"},
	{ID: "duration", Group: GroupBasic, Description: "A length of time"},
	{ID: "enum", Group: GroupChoices, Description: "One of its options {id, name, color}"},
	{ID: "multiEnum", Group: GroupChoices, Description: "Any of its options {id, name, color}"},
	{ID: "tags", Group: GroupChoices, Description: "Free-form tags"},
	{ID: "relation", Group: GroupReferences, Description: "A record (or with many, records) of another entity type: relation {entity, many, inverse}; entity \"user\" is the tenant's users"},
	{ID: "user", Group: GroupReferences, Description: "One of the tenant's users"},
	{ID: "file", Group: GroupReferences, Description: "An uploaded file"},
	{ID: "volumeFile", Group: GroupReferences, Description: "A file in one of the tenant's Volumes"},
	{ID: "json", Group: GroupStructured, Description: "Any JSON value, following schema when it has one"},
	{ID: "list", Group: GroupStructured, Description: "A list of values, each as items describes ({type, …})"},
	{ID: "object", Group: GroupStructured, Description: "Nested fields"},
	{ID: "formula", Group: GroupComputed, Computed: true, Description: "Computed from the record by formula, an expression"},
	{ID: "rollup", Group: GroupComputed, Computed: true, Description: "An aggregate over a relation: rollup {relation, field, aggregate: count|sum|min|max|avg}"},
	{ID: "autoNumber", Group: GroupComputed, Computed: true, Description: "A number given to each new record, counting up"},
	{ID: "createdAt", Group: GroupComputed, Computed: true, Description: "When the record was made"},
	{ID: "updatedAt", Group: GroupComputed, Computed: true, Description: "When the record last changed"},
	{ID: "createdBy", Group: GroupComputed, Computed: true, Description: "The user who made the record"},
}

// FieldTypeOf finds a field type.
func FieldTypeOf(id string) (FieldType, bool) {
	for _, t := range FieldTypes {
		if t.ID == id {
			return t, true
		}
	}
	return FieldType{}, false
}
