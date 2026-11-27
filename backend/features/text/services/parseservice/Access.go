package parseservice

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Action is what parsing needs: every signed-in user, app and agent may
// parse files they uploaded themselves.
const Action = "parse.file"

// ContributeAccess declares the parse resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "parse", Source: "parse", Order: 95}, Title: "File parsing", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "file", Title: "Parse files you uploaded (such as a CSV to import) into data", Class: authz.ClassSelf, Apps: true, Agents: true},
		},
	})
}
