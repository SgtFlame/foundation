package userhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/identity/rest/userdto"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// List handles POST /api/v1/identity/users/list.
func (uh *UserHandler) List(c *gin.Context) {
	resp := uh.service.List(userservice.ListArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), userdto.ListResponseFrom(resp))
}

// Create handles POST /api/v1/identity/users/create.
func (uh *UserHandler) Create(c *gin.Context) {
	var req userdto.CreateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := uh.service.Create(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), userdto.UserResponseFrom(resp.Common, resp.User))
}

// Update handles POST /api/v1/identity/users/update.
func (uh *UserHandler) Update(c *gin.Context) {
	var req userdto.UpdateRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := uh.service.Update(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), userdto.UserResponseFrom(resp.Common, resp.User))
}
