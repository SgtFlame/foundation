package tenantdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
)

type CreateRequest struct {
	Name string `json:"name"`
}

func (r CreateRequest) ToArgs(args features.ServiceArgs) tenantservice.CreateArgs {
	return tenantservice.CreateArgs{ServiceArgs: args, Name: r.Name}
}

type RenameRequest struct {
	TenantID uint64 `json:"tenantId,string"`
	Name     string `json:"name"`
}

func (r RenameRequest) ToArgs(args features.ServiceArgs) tenantservice.RenameArgs {
	return tenantservice.RenameArgs{ServiceArgs: args, TenantID: r.TenantID, Name: r.Name}
}

func (r TenantID) ToDeleteArgs(args features.ServiceArgs) tenantservice.DeleteArgs {
	return tenantservice.DeleteArgs{ServiceArgs: args, TenantID: r.TenantID}
}

func (r TenantID) ToMembersArgs(args features.ServiceArgs) tenantservice.MembersArgs {
	return tenantservice.MembersArgs{ServiceArgs: args, TenantID: r.TenantID}
}

func (r Member) ToAddArgs(args features.ServiceArgs) tenantservice.AddMemberArgs {
	return tenantservice.AddMemberArgs{ServiceArgs: args, TenantID: r.TenantID, UserID: r.UserID}
}

func (r Member) ToRemoveArgs(args features.ServiceArgs) tenantservice.RemoveMemberArgs {
	return tenantservice.RemoveMemberArgs{ServiceArgs: args, TenantID: r.TenantID, UserID: r.UserID}
}
