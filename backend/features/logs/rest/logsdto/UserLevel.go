package logsdto

import (
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// UserLevel is full logging for one user.
type UserLevel struct {
	UserID    uint64    `json:"userId,string"`
	Level     string    `json:"level"`
	SetBy     uint64    `json:"setBy,string"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func UserLevelFrom(l logs.UserLogLevel) UserLevel {
	return UserLevel{UserID: l.UserID, Level: l.Level, SetBy: l.SetBy, ExpiresAt: l.ExpiresAt}
}

type SetUserLevelRequest struct {
	UserID uint64 `json:"userId,string"`
	Level  string `json:"level"` // "debug" or ""
	Hours  int    `json:"hours,omitempty"`
}

func (r SetUserLevelRequest) ToArgs(args features.ServiceArgs) logsservice.SetUserLevelArgs {
	return logsservice.SetUserLevelArgs{ServiceArgs: args, UserID: r.UserID, Level: r.Level, Hours: r.Hours}
}

type SetUserLevelResponse struct {
	rest.Status
	Level *UserLevel `json:"level,omitempty"`
}

func SetUserLevelResponseFrom(resp logsservice.SetUserLevelResponse) SetUserLevelResponse {
	out := SetUserLevelResponse{Status: rest.StatusFrom(resp.Common)}
	if resp.Level != nil {
		l := UserLevelFrom(*resp.Level)
		out.Level = &l
	}
	return out
}

type UserLevelsResponse struct {
	rest.Status
	Levels []UserLevel `json:"levels"`
}

func UserLevelsResponseFrom(resp logsservice.UserLevelsResponse) UserLevelsResponse {
	out := UserLevelsResponse{Status: rest.StatusFrom(resp.Common), Levels: make([]UserLevel, len(resp.Levels))}
	for i, l := range resp.Levels {
		out.Levels[i] = UserLevelFrom(l)
	}
	return out
}

type LevelResponse struct {
	rest.Status
	BrowserLevel string     `json:"browserLevel"`
	Until        *time.Time `json:"until,omitempty"`
}

func LevelResponseFrom(resp logsservice.LevelResponse) LevelResponse {
	out := LevelResponse{Status: rest.StatusFrom(resp.Common), BrowserLevel: strings.ToLower(resp.BrowserLevel.String())}
	if !resp.Until.IsZero() {
		out.Until = &resp.Until
	}
	return out
}
