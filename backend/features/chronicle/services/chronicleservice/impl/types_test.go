package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// TestStoryBibleTypes is forge 35f-6: a project copies a starter set, its
// entries keep typed fields and links as facts, its readers find them and
// others do not.
func TestStoryBibleTypes(t *testing.T) {
	s, store, _ := newTestService(t)
	const bible = "project:bible"
	chronicle.SetProjectAccess(func(_ context.Context, p features.Principal) []string {
		if p.UserID == 2 {
			return nil
		}
		return []string{bible}
	})
	defer chronicle.SetProjectAccess(nil)
	as := func(user uint64) features.ServiceArgs {
		return features.ServiceArgs{Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
	}

	// A project the principal does not read is refused.
	if code := s.CopyStarter(chronicleservice.CopyStarterArgs{ServiceArgs: as(2), Starter: "fantasy", Scope: bible}).Common.Code; code != http.StatusNotFound {
		t.Fatalf("copy into an unread project: %d", code)
	}
	types := s.CopyStarter(chronicleservice.CopyStarterArgs{ServiceArgs: me(), Starter: "fantasy", Scope: bible})
	if types.Common.Code != http.StatusOK || !slices.ContainsFunc(types.Types, func(t chronicle.EntryType) bool { return t.ID == "character" && t.Scope == bible }) {
		t.Fatalf("copy starter: %+v", types.Common)
	}
	if len(types.Starters) != 2 {
		t.Errorf("starters %d", len(types.Starters))
	}

	// A tenant type with a field: problems name paths.
	bad := s.SaveType(chronicleservice.SaveTypeArgs{ServiceArgs: me(), Type: chronicle.EntryType{ID: "spell", Title: "Spells", Scope: chronicle.ScopeTenant,
		RecordFields: []records.Field{{Name: "School", Type: "nope"}}}})
	if bad.Common.Code != http.StatusBadRequest || len(bad.Problems) == 0 || !strings.HasPrefix(bad.Problems[0].Path, "fields.school") {
		t.Errorf("bad type: %+v %+v", bad.Common, bad.Problems)
	}
	if code := s.SaveType(chronicleservice.SaveTypeArgs{ServiceArgs: me(), Type: chronicle.EntryType{ID: "note", Title: "Notes", Scope: chronicle.ScopeTenant}}).Common.Code; code != http.StatusBadRequest {
		t.Errorf("a built-in id: %d", code)
	}

	// Entries of the project's types, with a relation and an enum.
	in := func(typ, title, fields string) chronicleservice.EntryInput {
		return chronicleservice.EntryInput{Type: typ, Title: title, Scope: bible, Fields: json.RawMessage(fields)}
	}
	city := create(t, s, in("place", "Greyhaven", `{"kind": "city"}`))
	wenlan := create(t, s, in("character", "Wenlan", `{"age": "31", "status": "alive", "home": "`+itoa(city.ID)+`"}`))
	sister := create(t, s, in("character", "Maera", `{}`))
	if !strings.Contains(wenlan.Facts, "Home: Greyhaven") || !strings.Contains(wenlan.Facts, "Status: Alive") {
		t.Fatalf("facts %q", wenlan.Facts)
	}
	if code := s.Create(chronicleservice.CreateArgs{ServiceArgs: me(), EntryInput: in("character", "X", `{"status": "zombie"}`)}).Common.Code; code != http.StatusBadRequest {
		t.Errorf("a bad option: %d", code)
	}
	if code := s.Create(chronicleservice.CreateArgs{ServiceArgs: as(2), EntryInput: in("note", "X", `{}`)}).Common.Code; code != http.StatusNotFound {
		t.Errorf("an entry in an unread project: %d", code)
	}
	if code := s.Create(chronicleservice.CreateArgs{ServiceArgs: me(), EntryInput: chronicleservice.EntryInput{Type: "character", Title: "Y"}}).Common.Code; code != http.StatusBadRequest {
		t.Errorf("a project's type outside it: %d", code)
	}

	// A typed link names the other entry in both entries' facts.
	if code := s.Link(chronicleservice.LinkArgs{ServiceArgs: me(), FromID: sister.ID, ToID: wenlan.ID, Kind: "sister of"}).Common.Code; code != http.StatusOK {
		t.Fatalf("link: %d", code)
	}
	s.indexing.Wait()
	var w, m chronicle.Entry
	s.db.First(&w, wenlan.ID)
	s.db.First(&m, sister.ID)
	if !strings.Contains(m.Facts, "sister of Wenlan") || !strings.Contains(w.Facts, "Maera is sister of this") {
		t.Errorf("link facts %q / %q", m.Facts, w.Facts)
	}
	if row := store.rows[wenlan.ID]; row.Scope != bible || !strings.Contains(row.Text, "Maera is sister of this") {
		t.Errorf("indexed row %+v", row)
	}

	// Renaming the city rewrites the facts that name it.
	if code := s.Update(chronicleservice.UpdateArgs{ServiceArgs: me(), ID: city.ID, EntryInput: in("place", "Grey Haven", `{"kind": "city"}`)}).Common.Code; code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
	s.indexing.Wait()
	s.db.First(&w, wenlan.ID)
	if !strings.Contains(w.Facts, "Home: Grey Haven") {
		t.Errorf("after the rename %q", w.Facts)
	}

	// The project's readers see its entries; others do not.
	visible := func(user uint64) int {
		list := s.List(chronicleservice.ListArgs{ServiceArgs: as(user)})
		return len(list.Entries)
	}
	if n := visible(3); n != 3 {
		t.Errorf("a reader sees %d entries", n)
	}
	if n := visible(2); n != 0 {
		t.Errorf("an outsider sees %d entries", n)
	}

	// A used type is not deleted.
	if code := s.DeleteType(chronicleservice.DeleteTypeArgs{ServiceArgs: me(), ID: "character", Scope: bible}).Common.Code; code != http.StatusConflict {
		t.Errorf("delete a used type: %d", code)
	}
	if code := s.DeleteType(chronicleservice.DeleteTypeArgs{ServiceArgs: me(), ID: "magicRule", Scope: bible}).Common.Code; code != http.StatusOK {
		t.Errorf("delete an unused type: %d", code)
	}
}

func itoa(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
