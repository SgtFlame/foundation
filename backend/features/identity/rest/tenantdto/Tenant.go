package tenantdto

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// Tenant is a tenant in the Tenant editor's list.
type Tenant struct {
	ID      uint64 `json:"id,string"`
	Name    string `json:"name"`
	Members int64  `json:"members"`
}

// TenantID names the tenant of an operation.
type TenantID struct {
	TenantID uint64 `json:"tenantId,string"`
}

// Member names a tenant and a user.
type Member struct {
	TenantID uint64 `json:"tenantId,string"`
	UserID   uint64 `json:"userId,string"`
}

type ListResponse struct {
	rest.Status
	Tenants []Tenant `json:"tenants"`
}

func ListResponseFrom(resp tenantservice.ListResponse) ListResponse {
	out := ListResponse{Status: rest.StatusFrom(resp.Common), Tenants: make([]Tenant, len(resp.Tenants))}
	for i, t := range resp.Tenants {
		out.Tenants[i] = Tenant{ID: t.ID, Name: t.Name, Members: t.Members}
	}
	return out
}

// TenantResponse is the response of Create and Rename.
type TenantResponse struct {
	rest.Status
	Tenant *identitydto.Tenant `json:"tenant,omitempty"`
}

// MemberUser is a member with the roles they hold on the tenant.
type MemberUser struct {
	identitydto.User
	Roles []string `json:"roles"`
}

type MembersResponse struct {
	rest.Status
	Users []MemberUser `json:"users"`
}

func MembersResponseFrom(resp tenantservice.MembersResponse) MembersResponse {
	out := MembersResponse{Status: rest.StatusFrom(resp.Common), Users: make([]MemberUser, len(resp.Users))}
	for i, u := range resp.Users {
		out.Users[i] = MemberUser{User: identitydto.UserFrom(u), Roles: resp.Roles[u.ID]}
		if out.Users[i].Roles == nil {
			out.Users[i].Roles = []string{}
		}
	}
	return out
}
