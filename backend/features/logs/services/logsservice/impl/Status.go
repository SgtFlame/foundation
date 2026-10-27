package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// Status tells whether logs are stored, and how storing is going.
func (s *service) Status(args logsservice.StatusArgs) logsservice.StatusResponse {
	const action = "logs.read"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return logsservice.StatusResponse{Common: *args.Fail(authz.Status, err)}
	}
	settings := s.settings(args.ServiceArgs)
	resp := logsservice.StatusResponse{Common: features.OK(), StoredLevel: settings.Stored(), BrowserLevel: settings.Browser()}
	if s.victoria != nil {
		st := s.victoria.Status()
		resp.Configured, resp.URL, resp.StoredLevel = st.Configured, st.URL, st.Level
		resp.Queued, resp.Dropped, resp.LastError, resp.LastErrorAt = st.Queued, st.Dropped, st.LastError, st.LastErrorAt
	}
	return resp
}
