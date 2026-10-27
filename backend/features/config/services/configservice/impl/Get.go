package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Get resolves a section as seen from a scope.
func (s *service) Get(args configservice.GetArgs) configservice.GetResponse {
	layers, f := chain(args.Principal, args.Scope)
	if f == nil {
		f = check(args.ServiceArgs, "config.read", args.Section, layers)
	}
	if f != nil {
		return configservice.GetResponse{Common: *args.Fail(f.status, f.err)}
	}
	sec, f := s.section(args.Section)
	if f != nil {
		return configservice.GetResponse{Common: *args.Fail(f.status, f.err)}
	}
	value, sources, row, f := s.resolved(sec, layers, nil)
	if f != nil {
		return configservice.GetResponse{Common: *args.Fail(f.status, f.err)}
	}
	return configservice.GetResponse{Common: features.OK(), View: s.view(sec, args.Scope, value, sources, row)}
}
