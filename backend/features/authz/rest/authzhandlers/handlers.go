package authzhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/authz/rest/authzdto"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// Me handles GET /api/v1/authz/me.
func (ah *AuthzHandler) Me(c *gin.Context) {
	resp := ah.service.Me(authzservice.MeArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), authzdto.MeResponseFrom(resp))
}

// ResourceTypes handles GET /api/v1/authz/resource-types.
func (ah *AuthzHandler) ResourceTypes(c *gin.Context) {
	resp := ah.service.ResourceTypes(authzservice.ResourceTypesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), authzdto.ResourceTypesResponseFrom(resp))
}

// Roles handles GET /api/v1/authz/roles.
func (ah *AuthzHandler) Roles(c *gin.Context) {
	resp := ah.service.Roles(authzservice.RolesArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), authzdto.RolesResponseFrom(resp))
}

// SaveRole handles POST /api/v1/authz/roles/save.
func (ah *AuthzHandler) SaveRole(c *gin.Context) {
	var req authzdto.SaveRoleRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.SaveRole(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), authzdto.RoleResponseFrom(resp))
}

// DeleteRole handles POST /api/v1/authz/roles/delete.
func (ah *AuthzHandler) DeleteRole(c *gin.Context) {
	var req authzdto.DeleteRoleRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.DeleteRole(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}

// Grants handles POST /api/v1/authz/grants.
func (ah *AuthzHandler) Grants(c *gin.Context) {
	var req authzdto.GrantsRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.Grants(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), authzdto.GrantsResponseFrom(resp))
}

// Grant handles POST /api/v1/authz/grant.
func (ah *AuthzHandler) Grant(c *gin.Context) {
	var req authzdto.GrantRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.Grant(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), authzdto.GrantResponseFrom(resp))
}

// Revoke handles POST /api/v1/authz/revoke.
func (ah *AuthzHandler) Revoke(c *gin.Context) {
	var req authzdto.RevokeRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.Revoke(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), rest.StatusFrom(resp.Common))
}

// Explain handles POST /api/v1/authz/explain.
func (ah *AuthzHandler) Explain(c *gin.Context) {
	var req authzdto.ExplainRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := ah.service.Explain(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), authzdto.ExplainResponseFrom(resp))
}
