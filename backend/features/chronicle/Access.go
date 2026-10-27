package chronicle

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the chronicle resource type. Entries belong to
// their owner, which chronicleservice's queries enforce, so every member
// keeps their own memory; moderating shares needs a role.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "chronicle", Source: "chronicle", Order: 60}, Title: "Chronicles", Parent: "tenant", Owned: true,
		Actions: []authz.Action{
			{Name: "read", Title: "Read your Chronicles and those shared with you", Class: authz.ClassSelf},
			{Name: "write", Title: "Change your Chronicles", Class: authz.ClassSelf},
			{Name: "share", Title: "Share your Chronicles", Class: authz.ClassSelf},
			{Name: "moderate", Title: "Approve shares out of the tenant", Class: authz.ClassModerate},
			{Name: "moderateGlobal", Title: "Approve shares to the global tenant", Class: authz.ClassModerate, On: authz.OnApp},
		},
	})
}
