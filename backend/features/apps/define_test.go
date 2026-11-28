package apps

import (
	"slices"
	"testing"
)

// entity:<id>.define is record.define on the type's records (forge
// 35c-1), never part of entity:<id>.*, and no command's action.
func TestEntityDefine(t *testing.T) {
	if e, acts, ok := EntityAction("entity:database.define"); !ok || e != "database" || !slices.Equal(acts, []string{"define"}) {
		t.Errorf("define: %v %v %v", e, acts, ok)
	}
	if _, acts, _ := EntityAction("entity:database.*"); slices.Contains(acts, "define") {
		t.Error("* includes define")
	}
	v := Version{Owned: []string{"database"}, Manifest: Manifest{ID: "docs", Roles: []Role{{ID: "member", Actions: []string{"entity:database.update", "entity:database.define"}}}}}
	roles := AppRoles(v)
	if len(roles) != 1 || !slices.Contains(roles[0].Permissions, "app:docs/entity:database/record.define") {
		t.Errorf("roles %+v", roles)
	}
}
