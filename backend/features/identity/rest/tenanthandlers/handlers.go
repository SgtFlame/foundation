package tenanthandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/identitydto"
	"github.com/SgtFlame/foundation/backend/features/identity/rest/tenantdto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// List handles POST /api/v1/identity/tenants/list.
func (th *TenantHandler) List(c *gin.Context) {
	resp := th.service.List(tenantservice.ListArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), tenantdto.ListResponseFrom(resp))
}

// Create handles POST /api/v1/identity/tenants/create.
func (th *TenantHandler) Create(c *gin.Context) {
	var req tenantdto.CreateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.Create(req.ToArgs(args))
	out := tenantdto.TenantResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		t := identitydto.TenantFrom(resp.Tenant)
		out.Tenant = &t
	}
	c.JSON(int(resp.Common.Code), out)
}

// Rename handles POST /api/v1/identity/tenants/rename.
func (th *TenantHandler) Rename(c *gin.Context) {
	var req tenantdto.RenameRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.Rename(req.ToArgs(args))
	out := tenantdto.TenantResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Common.Code < 300 {
		t := identitydto.TenantFrom(resp.Tenant)
		out.Tenant = &t
	}
	c.JSON(int(resp.Common.Code), out)
}

// Delete handles POST /api/v1/identity/tenants/delete.
func (th *TenantHandler) Delete(c *gin.Context) {
	var req tenantdto.TenantID
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.Delete(req.ToDeleteArgs(args))
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}

// Members handles POST /api/v1/identity/tenants/members.
func (th *TenantHandler) Members(c *gin.Context) {
	var req tenantdto.TenantID
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.Members(req.ToMembersArgs(args))
	c.JSON(int(resp.Common.Code), tenantdto.MembersResponseFrom(resp))
}

// AddMember handles POST /api/v1/identity/tenants/add-member.
func (th *TenantHandler) AddMember(c *gin.Context) {
	var req tenantdto.Member
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.AddMember(req.ToAddArgs(args))
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}

// RemoveMember handles POST /api/v1/identity/tenants/remove-member.
func (th *TenantHandler) RemoveMember(c *gin.Context) {
	var req tenantdto.Member
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := th.service.RemoveMember(req.ToRemoveArgs(args))
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}
