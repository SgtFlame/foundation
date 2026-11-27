package authzservice

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Remote is an authz.Source reading through svc (a proxy, forge 21d-1),
// as the service principal of service.
func Remote(svc Service, service string) authz.Source { return remote{svc, service} }

type remote struct {
	svc     Service
	service string
}

func (r remote) args(ctx context.Context) features.ServiceArgs {
	return features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(features.LocalTenantID, r.service)}
}

func (r remote) CustomRoles(ctx context.Context) ([]authz.CustomRole, error) {
	resp := r.svc.ReadRoles(ReadRolesArgs{ServiceArgs: r.args(ctx)})
	if resp.Common.Code != 200 {
		return nil, logging.Code(resp.Common.ErrorCode).Errorf("cannot read the custom roles: %s", resp.Common.Message)
	}
	return resp.Roles, nil
}

func (r remote) Grants(ctx context.Context, agent bool, id uint64) ([]authz.RoleGrant, error) {
	resp := r.svc.ReadGrants(ReadGrantsArgs{ServiceArgs: r.args(ctx), Agent: agent, ID: id})
	if resp.Common.Code != 200 {
		return nil, logging.Code(resp.Common.ErrorCode).Errorf("cannot read grants: %s", resp.Common.Message)
	}
	return resp.Grants, nil
}
