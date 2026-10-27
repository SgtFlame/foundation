package impl

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/logs"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// loadUserLevels gives Victoria the users' own levels that have not
// expired, removing those that have.
func (s *service) loadUserLevels(ctx context.Context) error {
	if s.db == nil || s.victoria == nil {
		return nil
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&logs.UserLogLevel{}).Error; err != nil {
		return err
	}
	var rows []logs.UserLogLevel
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	levels := make(map[string]logging.UserLevel, len(rows))
	for _, r := range rows {
		if level, err := logging.ParseLevel(r.Level); err == nil {
			levels[strconv.FormatUint(r.UserID, 10)] = logging.UserLevel{Level: level, Until: r.ExpiresAt}
		}
	}
	s.victoria.SetUserLevels(levels)
	return nil
}

// SetUserLevel turns full logging for one user on or off.
func (s *service) SetUserLevel(args logsservice.SetUserLevelArgs) logsservice.SetUserLevelResponse {
	const action = "logs.setUserLevel"
	resource := authz.Resource("user", args.UserID)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(authz.Status, err)}
	}
	if s.db == nil {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusServiceUnavailable, log.New("user log levels are not stored"))}
	}
	if args.Level != "" && args.Level != "debug" {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("bad_level").Public("Full logging is the debug level").Errorf("level %q is not debug", args.Level))}
	}
	hours := args.Hours
	if hours == 0 {
		hours = logsservice.DefaultUserLevelHours
	}
	if hours < 0 || hours > logsservice.MaxUserLevelHours {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("bad_hours").Public("Full logging lasts 1 to 168 hours").Errorf("%d hours", hours))}
	}
	q := s.db.WithContext(args.Context())
	if err := q.Where("user_id = ?", args.UserID).Delete(&logs.UserLogLevel{}).Error; err != nil {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var row *logs.UserLogLevel
	if args.Level != "" {
		row = &logs.UserLogLevel{UserID: args.UserID, Level: args.Level, SetBy: args.Principal.UserID, ExpiresAt: time.Now().Add(time.Duration(hours) * time.Hour)}
		if err := s.db.WithContext(args.Context()).Create(row).Error; err != nil {
			return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
	}
	if err := s.loadUserLevels(args.Context()); err != nil {
		return logsservice.SetUserLevelResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	log.With("userId", args.UserID, "level", args.Level, "hours", hours).Info(args.Context(), "full logging for one user set")
	return logsservice.SetUserLevelResponse{Common: features.OK(), Level: row}
}

// UserLevels lists the users with full logging on.
func (s *service) UserLevels(args logsservice.UserLevelsArgs) logsservice.UserLevelsResponse {
	const action = "logs.setUserLevel"
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return logsservice.UserLevelsResponse{Common: *args.Fail(authz.Status, err)}
	}
	if s.db == nil {
		return logsservice.UserLevelsResponse{Common: features.OK()}
	}
	var rows []logs.UserLogLevel
	if err := s.db.WithContext(args.Context()).Where("expires_at > ?", time.Now()).Order("user_id").Find(&rows).Error; err != nil {
		return logsservice.UserLevelsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return logsservice.UserLevelsResponse{Common: features.OK(), Levels: rows}
}

// Level is the level the principal's browser forwards from.
func (s *service) Level(args logsservice.LevelArgs) logsservice.LevelResponse {
	const action = "logs.ingest"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return logsservice.LevelResponse{Common: *args.Fail(authz.Status, err)}
	}
	resp := logsservice.LevelResponse{Common: features.OK(), BrowserLevel: s.settings(args.ServiceArgs).Browser()}
	if s.victoria == nil {
		return resp
	}
	if own, ok := s.victoria.UserLevel(strconv.FormatUint(args.Principal.UserID, 10)); ok && own < resp.BrowserLevel {
		resp.BrowserLevel = own
		var row logs.UserLogLevel
		if s.db != nil && s.db.WithContext(args.Context()).Where("user_id = ?", args.Principal.UserID).First(&row).Error == nil {
			resp.Until = row.ExpiresAt
		}
	}
	return resp
}
