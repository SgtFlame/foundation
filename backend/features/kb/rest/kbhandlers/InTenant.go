package kbhandlers

import (
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// InTenant refuses the routes of Volumes, their index and collaboration to
// a tenant that has no Volumes (kbservice.InTenant), with 403
// volumes_unavailable. serve.go puts it on their router group.
func InTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		args := rest.Args(c)
		if kbservice.InTenant(args.Principal) {
			c.Next()
			return
		}
		failed := args.Fail(authz.Status, kbservice.ErrOtherTenant())
		c.AbortWithStatusJSON(authz.Status, rest.StatusFrom(*failed))
	}
}
