package apps

import (
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/authz"
)

// RecordActions are what an action on an entity type grants of its
// records: entity:task.read is record.read on task's records, and
// entity:task.* all four.
var RecordActions = []string{"read", "create", "update", "delete"}

// EntityAction splits an action a role or a command names,
// entity:<id>.<action>, into the entity type and its record actions.
func EntityAction(a string) (entity string, actions []string, ok bool) {
	res, act, ok := strings.Cut(a, ".")
	entity, isEntity := strings.CutPrefix(res, "entity:")
	switch {
	case !ok || !isEntity || entity == "":
		return "", nil, false
	case act == "*":
		return entity, RecordActions, true
	case slices.Contains(RecordActions, act):
		return entity, []string{act}, true
	}
	return "", nil, false
}

// Path is where an app's own entity type is below the tenant while the
// app is installed: app:<appId>/entity:<id> (forge 05c question 3).
func Path(app, entity string) string { return "app:" + app + "/entity:" + entity }

// AppRoles are the custom roles an install makes of the version's roles
// (forge 05c): each action on one of the app's own entity types becomes
// record.<action> scoped to that type's path, so the roles reach nothing
// beyond the app (question 1), and each role's row rules go with it
// (question 4).
func AppRoles(v Version) []authz.AppRole {
	m := v.Manifest
	out := make([]authz.AppRole, 0, len(m.Roles))
	for _, r := range m.Roles {
		ar := authz.AppRole{ID: r.ID, Name: firstNonEmpty(r.Title, r.ID), Description: r.Description, Permissions: []string{}, Rules: r.RowRules}
		if ar.Description == "" {
			ar.Description = "A role of the app " + m.Name
		}
		for _, a := range r.Actions {
			entity, acts, ok := EntityAction(a)
			if !ok || !slices.Contains(v.Owned, entity) {
				continue
			}
			for _, act := range acts {
				p := Path(m.ID, entity) + "/" + authz.Permission("record", act)
				if !slices.Contains(ar.Permissions, p) {
					ar.Permissions = append(ar.Permissions, p)
				}
			}
		}
		out = append(out, ar)
	}
	return out
}

// InstallerRole is the role the installer is given (forge 05c): the one
// the manifest marks, else its first; empty when it has none.
func InstallerRole(m Manifest) string {
	for _, r := range m.Roles {
		if r.Installer {
			return r.ID
		}
	}
	if len(m.Roles) > 0 {
		return m.Roles[0].ID
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
