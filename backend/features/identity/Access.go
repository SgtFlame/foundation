package identity

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares identity's resource types: tenants, users and
// the user's own account.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "tenant", Source: "identity", Order: 10}, Title: "Tenants", Parent: "app",
		Actions: []authz.Action{
			{Name: "list", Title: "List every tenant", Class: authz.ClassRead, On: authz.OnApp},
			{Name: "create", Title: "Create tenants", Class: authz.ClassCreate, On: authz.OnApp},
			{Name: "read", Title: "See the tenant and its members", Class: authz.ClassRead},
			{Name: "update", Title: "Rename the tenant", Class: authz.ClassAdmin},
			{Name: "delete", Title: "Delete the tenant", Class: authz.ClassDelete},
			{Name: "addMember", Title: "Add members", Class: authz.ClassAdmin},
			{Name: "removeMember", Title: "Remove members", Class: authz.ClassAdmin},
		},
	})
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "user", Source: "identity", Order: 11}, Title: "Users", Parent: "app",
		Actions: []authz.Action{
			{Name: "list", Title: "List users", Class: authz.ClassAdmin, On: authz.OnApp},
			{Name: "create", Title: "Create users", Class: authz.ClassAdmin, On: authz.OnApp},
			{Name: "update", Title: "Change, disable or reset users", Class: authz.ClassAdmin, On: authz.OnApp},
		},
	})
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "account", Source: "identity", Order: 12}, Title: "Your account", Parent: "app",
		Actions: []authz.Action{
			{Name: "update", Title: "Change your profile and password", Class: authz.ClassSelf, On: authz.OnApp},
			{Name: "switchTenant", Title: "Switch between your tenants", Class: authz.ClassSelf, On: authz.OnApp},
		},
	})
}
