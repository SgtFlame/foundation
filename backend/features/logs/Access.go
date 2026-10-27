package logs

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the logs resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "logs", Source: "logs", Order: 30}, Title: "Logs", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "Read the tenant's logs", Class: authz.ClassAdmin},
			{Name: "ingest", Title: "Forward your browser's logs", Class: authz.ClassSelf},
			{Name: "readAll", Title: "Read every tenant's logs", Class: authz.ClassAdmin, On: authz.OnApp},
			{Name: "setUserLevel", Title: "Turn on full logging for a user", Class: authz.ClassAdmin, On: authz.OnApp},
		},
	})
}
