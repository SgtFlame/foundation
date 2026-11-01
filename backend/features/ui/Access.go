package ui

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the ui resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "ui", Source: "ui", Order: 90}, Title: "Applications", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See the installed applications and the component catalog", Class: authz.ClassRead},
			{Name: "design", Title: "Change applications' pages, such as adding a button", Class: authz.ClassWrite},
		},
	})
}
