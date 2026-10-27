package authzdto

import (
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type MeResponse struct {
	rest.Status
	Effective
}

func MeResponseFrom(resp authzservice.MeResponse) MeResponse {
	return MeResponse{Status: rest.StatusFrom(resp.Common), Effective: EffectiveFrom(resp.Effective)}
}

type ResourceTypesResponse struct {
	rest.Status
	Types []ResourceType `json:"types"`
}

func ResourceTypesResponseFrom(resp authzservice.ResourceTypesResponse) ResourceTypesResponse {
	out := ResourceTypesResponse{Status: rest.StatusFrom(resp.Common), Types: make([]ResourceType, len(resp.Types))}
	for i, t := range resp.Types {
		out.Types[i] = ResourceTypeFrom(t)
	}
	return out
}

type RolesResponse struct {
	rest.Status
	Roles []Role `json:"roles"`
}

func RolesResponseFrom(resp authzservice.RolesResponse) RolesResponse {
	out := RolesResponse{Status: rest.StatusFrom(resp.Common), Roles: make([]Role, len(resp.Roles))}
	for i, r := range resp.Roles {
		out.Roles[i] = RoleFrom(r)
	}
	return out
}

type RoleResponse struct {
	rest.Status
	Role *Role `json:"role,omitempty"`
}

func RoleResponseFrom(resp authzservice.SaveRoleResponse) RoleResponse {
	out := RoleResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		r := RoleFrom(resp.Role)
		out.Role = &r
	}
	return out
}

type GrantsResponse struct {
	rest.Status
	Grants []Grant `json:"grants"`
}

func GrantsResponseFrom(resp authzservice.GrantsResponse) GrantsResponse {
	return GrantsResponse{Status: rest.StatusFrom(resp.Common), Grants: GrantsFrom(resp.Grants)}
}

type GrantResponse struct {
	rest.Status
	Grant *Grant `json:"grant,omitempty"`
}

func GrantResponseFrom(resp authzservice.GrantResponse) GrantResponse {
	out := GrantResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		g := GrantFrom(resp.Grant)
		out.Grant = &g
	}
	return out
}

type ExplainResponse struct {
	rest.Status
	Decision Decision `json:"decision"`
}

func ExplainResponseFrom(resp authzservice.ExplainResponse) ExplainResponse {
	return ExplainResponse{Status: rest.StatusFrom(resp.Common), Decision: DecisionFrom(resp.Decision)}
}
