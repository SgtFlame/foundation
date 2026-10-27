package kbservice

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// ContributeAccess declares the knowledge base's resource types: the
// folders outside Volumes ("tenant:1/kb") and each Volume
// ("tenant:1/volume:<id>"), whose grants also cover its files.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "kb", Source: "kb", Order: 90}, Title: "Knowledge base", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "Browse the folders", Class: authz.ClassRead},
			{Name: "write", Title: "Make folders and Volumes", Class: authz.ClassWrite},
		},
	})
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "volume", Source: "kb", Order: 91}, Title: "Volumes", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "Open the Volume and its files", Class: authz.ClassRead},
			{Name: "write", Title: "Edit its files and make folders and files", Class: authz.ClassWrite},
		},
	})
}
