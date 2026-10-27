package logshandlers

import (
	"github.com/SgtFlame/foundation/backend/features/logs/rest/logsdto"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
	"github.com/gin-gonic/gin"
)

// SetUserLevel handles POST /api/v1/logs/user-level.
func (lh *LogsHandler) SetUserLevel(c *gin.Context) {
	var req logsdto.SetUserLevelRequest
	args, ok := rest.Bind(c, &req)
	if !ok {
		return
	}
	resp := lh.service.SetUserLevel(req.ToArgs(args))
	c.JSON(int(resp.Common.Code), logsdto.SetUserLevelResponseFrom(resp))
}

// UserLevels handles POST /api/v1/logs/user-levels.
func (lh *LogsHandler) UserLevels(c *gin.Context) {
	resp := lh.service.UserLevels(logsservice.UserLevelsArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), logsdto.UserLevelsResponseFrom(resp))
}

// Level handles GET /api/v1/logs/level.
func (lh *LogsHandler) Level(c *gin.Context) {
	resp := lh.service.Level(logsservice.LevelArgs{ServiceArgs: rest.Args(c)})
	c.JSON(int(resp.Common.Code), logsdto.LevelResponseFrom(resp))
}
