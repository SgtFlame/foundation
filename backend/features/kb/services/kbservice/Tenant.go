package kbservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Volumes, their index and their collaboration rooms belong to the default
// tenant until each tenant has its own knowledge base root
// (documentation/plans/MultiTenant.md › Built in slice 10). Other tenants
// see no Volumes: the REST routes refuse them, the library source finds
// nothing, and the kb tools fail.

// InTenant tells whether p's tenant has the Volumes. Background work does.
func InTenant(p features.Principal) bool {
	return p.Kind == features.PrincipalService || p.TenantID == features.LocalTenantID
}

// ErrOtherTenant is the error of using Volumes from another tenant.
func ErrOtherTenant() error {
	return logging.In("kb").Code("volumes_unavailable").
		Public("Volumes are only available in the default tenant for now").
		Errorf("Volumes are only available in the default tenant")
}
