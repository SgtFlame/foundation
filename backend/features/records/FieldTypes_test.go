package records_test

import (
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/records"
)

// Every field type has the metadata a model needs (forge 06d), and its
// example is a field an entity type may hold: in one with the fields a
// formula's and a rollup's examples name.
func TestFieldTypesMetadata(t *testing.T) {
	known := func(id string) bool { return id == "line" || id == "customer" }
	for _, ft := range records.FieldTypes {
		if ft.Title == "" || ft.Description == "" || len(ft.Tags) == 0 || len(ft.Examples) == 0 {
			t.Errorf("%s lacks a title, description, tags or an example", ft.ID)
			continue
		}
		var f records.Field
		if err := json.Unmarshal(ft.Examples[0], &f); err != nil {
			t.Errorf("%s's example: %v", ft.ID, err)
			continue
		}
		if f.Type != ft.ID {
			t.Errorf("%s's example is a %s field", ft.ID, f.Type)
		}
		f.ID = "example"
		e := records.EntityType{ID: "order", Name: "Order", Fields: []records.Field{
			{ID: "quantity", Name: "Units", Type: "number"},
			{ID: "price", Name: "Unit price", Type: "number"},
			{ID: "lines", Name: "Order lines", Type: "relation", Relation: &records.Relation{Entity: "line", Many: true}},
			f,
		}}
		if p := records.Check(e, known); len(p) > 0 {
			t.Errorf("%s's example: %+v", ft.ID, p)
		}
	}
}
