package authzdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

type SaveRoleRequest struct {
	RoleID      uint64   `json:"roleId,string,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Revision    int      `json:"revision"`
}

func (r SaveRoleRequest) ToArgs(args features.ServiceArgs) authzservice.SaveRoleArgs {
	return authzservice.SaveRoleArgs{ServiceArgs: args, RoleID: r.RoleID, Name: r.Name, Description: r.Description, Permissions: r.Permissions, Revision: r.Revision}
}

type DeleteRoleRequest struct {
	RoleID uint64 `json:"roleId,string"`
}

func (r DeleteRoleRequest) ToArgs(args features.ServiceArgs) authzservice.DeleteRoleArgs {
	return authzservice.DeleteRoleArgs{ServiceArgs: args, RoleID: r.RoleID}
}

type GrantsRequest struct {
	Resource string `json:"resource,omitempty"`
	Under    bool   `json:"under,omitempty"`
	RoleID   uint64 `json:"roleId,string,omitempty"`
}

func (r GrantsRequest) ToArgs(args features.ServiceArgs) authzservice.GrantsArgs {
	return authzservice.GrantsArgs{ServiceArgs: args, Resource: r.Resource, Under: r.Under, RoleID: r.RoleID}
}

type GrantRequest struct {
	UserID    uint64     `json:"userId,string"`
	RoleID    uint64     `json:"roleId,string"`
	Resource  string     `json:"resource"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (r GrantRequest) ToArgs(args features.ServiceArgs) authzservice.GrantArgs {
	return authzservice.GrantArgs{ServiceArgs: args, UserID: r.UserID, RoleID: r.RoleID, Resource: r.Resource, ExpiresAt: r.ExpiresAt}
}

type RevokeRequest struct {
	GrantID uint64 `json:"grantId,string"`
}

func (r RevokeRequest) ToArgs(args features.ServiceArgs) authzservice.RevokeArgs {
	return authzservice.RevokeArgs{ServiceArgs: args, GrantID: r.GrantID}
}

type ExplainRequest struct {
	UserID   uint64 `json:"userId,string,omitempty"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

func (r ExplainRequest) ToArgs(args features.ServiceArgs) authzservice.ExplainArgs {
	return authzservice.ExplainArgs{ServiceArgs: args, UserID: r.UserID, Action: r.Action, Resource: r.Resource}
}
