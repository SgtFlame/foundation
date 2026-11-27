// Package infra is the runtime of forge iteration 22, Infrastructure as
// Code (documentation/Infrastructure.md): the deployment described by
// versioned configurations in the root tenant, planned against what exists
// and applied through the Control Plane and the infra.compute contract.
//
// Configurations are a definition family on the def stream (target type
// infraConfig, forge 22a question 1 settled as a definition family): the
// draft is one document changed by infra.config.* ops, each with its
// inverse, and committing it freezes an infraVersion.
package infra

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("infra")

const source = "infra"

// Target types, projectors and the area of plans and applies.
const (
	ConfigType       = "infraConfig"
	VersionType      = "infraVersion"
	ConfigProjector  = "infra.config"
	VersionProjector = "infra.version"
	// DefaultConfig is the one configuration a root tenant has (forge 22a
	// question 2: one draft).
	DefaultConfig = "main"
	// Area holds plans, applies and their steps: state, not definitions,
	// so they are not ops.
	Area live.Area = "infra"
)

// The actions infra checks (declared by controlplane.ContributeAccess on
// the infra resource type, pinned to the root tenant).
const (
	ActionRead    = "infra.read"
	ActionOperate = "infra.operate"
	ActionAdmin   = "infra.admin"
	ActionApprove = "infra.approve"
)

// checkRead refuses a principal without infra.read in its tenant.
func checkRead(args features.ServiceArgs) error {
	res := authz.Tenant(args.Principal)
	return authz.Check(args.Context(), args.Principal, ActionRead, res).Err(ActionRead, res)
}

// rootOnly is the problem for a command sent outside the root tenant.
func rootOnly(p features.Principal) []live.Problem {
	if p.TenantID == features.RootTenantID {
		return nil
	}
	return problem("not_root_tenant", "", "infrastructure lives only in the root tenant", "sign in to the Infrastructure tenant")
}

func problem(code, path, message, fix string) []live.Problem {
	return []live.Problem{{Code: code, Path: path, Message: message, Fix: fix}}
}
