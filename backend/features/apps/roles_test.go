package apps

import (
	"slices"
	"testing"
)

// entity:<id>.use gives agent.use on the type's records (forge 7p-2);
// entity:<id>.* gives the record actions alone; a command may not name use.
func TestAgentUseAction(t *testing.T) {
	v := Version{Owned: []string{"mailAgent"}, Manifest: Manifest{ID: "mail", Name: "Mail", Entities: []string{"mailAgent"},
		Roles: []Role{{ID: "user", Actions: []string{"entity:mailAgent.read", "entity:mailAgent.use"}}, {ID: "admin", Actions: []string{"entity:mailAgent.*"}}}}}
	roles := AppRoles(v)
	if !slices.Contains(roles[0].Permissions, "app:mail/entity:mailAgent/agent.use") || !slices.Contains(roles[0].Permissions, "app:mail/entity:mailAgent/record.read") {
		t.Errorf("user: %v", roles[0].Permissions)
	}
	if slices.Contains(roles[1].Permissions, "app:mail/entity:mailAgent/agent.use") || len(roles[1].Permissions) != 4 {
		t.Errorf("admin: %v", roles[1].Permissions)
	}
	if _, acts, ok := EntityAction("entity:mailAgent.use"); !ok || !slices.Equal(acts, []string{AgentUse}) {
		t.Errorf("EntityAction: %v %v", acts, ok)
	}
	c := &check{m: Manifest{ID: "mail", Entities: []string{"mailAgent"}, Commands: []Command{{Type: "agent.run", Action: "entity:mailAgent.use"}}}}
	c.commands()
	found := false
	for _, p := range c.problems {
		if p.Path == "commands[agent.run].action" {
			found = true
		}
	}
	if !found {
		t.Errorf("a command naming use was not refused: %+v", c.problems)
	}
}
