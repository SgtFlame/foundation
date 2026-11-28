package chroniclesteps

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// TestUpdateFields is forge 35f-6: the memory update reads a custom type's
// fields and an entry's values, and changes only the values it names,
// checked against the type.
func TestUpdateFields(t *testing.T) {
	f := newFixture(t)
	p := features.LocalPrincipal()
	typ := chronicle.EntryType{ID: "character", Title: "Characters", Scope: chronicle.ScopeTenant, RecordFields: []records.Field{
		{Name: "Age", Type: "text"}, {Name: "Status", Type: "enum", Options: []records.Option{{ID: "alive", Name: "Alive"}, {ID: "dead", Name: "Dead"}}}}}
	if problems := chronicle.CheckType(&typ); len(problems) > 0 {
		t.Fatal(problems)
	}
	fields, _ := json.Marshal(typ.RecordFields)
	f.db.Create(&chronicle.TypeDef{Tenanted: db.TenantOf(p), TypeID: typ.ID, Scope: typ.Scope, Title: typ.Title, Fields: string(fields), ParentTypes: "[]"})
	wenlan := chronicle.Entry{Tenanted: db.TenantOf(p), Owned: db.OwnedBy(p), Type: "character", Title: "Wenlan", ShortSummary: "A smith.",
		FullSummary: "A smith.", Fields: `{"age":"31","status":"alive"}`, Status: chronicle.StatusActive}
	f.db.Create(&wenlan)

	id := strconv.FormatUint(wenlan.ID, 10)
	f.chat.replies["chronicle-memory"] = `{"updates": [{"id": "` + id + `", "shortSummary": "", "fullSummary": "", "fields": "{\"status\": \"dead\"}"}],
	  "proposals": [{"type": "character", "title": "Maera", "shortSummary": "Her sister.", "fullSummary": "Her sister.", "fields": "{\"status\": \"zombie\"}"}]}`
	round := conversation.Round{Model: db.Model{ID: 5}, ConversationID: 7, Text: "Wenlan dies in chapter 9.", Reply: "Noted."}
	changes, err := f.steps.Update(UpdateIn{ServiceArgs: f.args(), Round: round, Used: []uint64{wenlan.ID}, MaxProposals: 1})
	if err != nil {
		t.Fatal(err)
	}
	call := f.chat.got[len(f.chat.got)-1]
	prompt := call.Messages[0].Content
	if !strings.Contains(prompt, `Fields: {"age":"31","status":"alive"}`) || !strings.Contains(prompt, "Status (status, enum) one of alive|dead") ||
		!strings.Contains(string(call.JSONSchema), `"character"`) {
		t.Errorf("the update does not see the fields:\n%s", prompt)
	}
	if len(changes.Updated) != 1 || changes.Updated[0].Fields != `{"age":"31","status":"dead"}` || changes.Updated[0].ShortSummary != "A smith." ||
		!strings.Contains(changes.Updated[0].Facts, "Status: Dead") {
		t.Errorf("updated %+v", changes.Updated)
	}
	if len(changes.Proposed) != 1 || changes.Proposed[0].Type != "character" || changes.Proposed[0].Fields != "{}" {
		t.Errorf("proposed %+v", changes.Proposed)
	}
	var round5 chronicle.EntryRound
	f.db.Where("entry_id = ? AND action = ?", wenlan.ID, chronicle.ActionUpdated).First(&round5)
	if !strings.Contains(round5.Previous, `"fields":"{\"age\":\"31\",\"status\":\"alive\"}"`) {
		t.Errorf("previous %s", round5.Previous)
	}
}
