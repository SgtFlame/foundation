// Package controlplane is the runtime of forge iteration 20 (the Control
// Plane, documentation/ControlPlane.md): the root tenant that alone holds
// infrastructure, its resource type and roles; natsauth below it is the
// deployment's NATS authorization.
package controlplane

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("controlplane")

// The infrastructure roles' reserved ids (contributed roles, 100 to 999).
const (
	RoleInfraViewer   uint64 = 110
	RoleInfraOperator uint64 = 111
	RoleInfraAdmin    uint64 = 112
)

// ContributeAccess declares the infra resource type, pinned to the root
// tenant (authz.ResourceType.Tenant: a role granted anywhere else, even
// the Super Administrator's, holds none of it), and the three
// infrastructure roles.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "infra", Source: "controlplane", Order: 95}, Title: "Infrastructure", Parent: "tenant",
		Tenant: features.RootTenantID,
		Actions: []authz.Action{
			{Name: "read", Title: "See hosts, instances, services and their state", Class: authz.ClassRead},
			{Name: "operate", Title: "Start, stop and restart services and instances", Class: authz.ClassExecute},
			{Name: "admin", Title: "Change desired state, clusters and credentials", Class: authz.ClassAdmin},
			{Name: "enroll", Title: "Enroll new hosts", Class: authz.ClassCreate},
			{Name: "approve", Title: "Approve enrollments and destructive changes", Class: authz.ClassModerate},
		},
	})
	for _, r := range []authz.Role{
		{ID: RoleInfraViewer, Name: "Infrastructure viewer", Description: "Sees the infrastructure and its state, in the root tenant",
			Permissions: []string{"infra.read"}},
		{ID: RoleInfraOperator, Name: "Infrastructure operator", Description: "Sees the infrastructure and starts, stops and restarts its services",
			Permissions: []string{"infra.read", "infra.operate"}},
		{ID: RoleInfraAdmin, Name: "Infrastructure administrator", Description: "Holds every infrastructure action: desired state, enrollment, approvals",
			Permissions: []string{"infra.read", "infra.operate", "infra.admin", "infra.enroll", "infra.approve"}},
	} {
		authz.ContributedRoles.Register(reg, authz.ContributedRole{Descriptor: extensions.Descriptor{ID: "infra.role." + r.Name, Source: "controlplane", Order: int(r.ID)}, Role: r})
	}
}
