package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Reset drops a scope's overrides of some keys, or of all of them, so their
// values are inherited again.
func (s *service) Reset(args configservice.ResetArgs) configservice.ResetResponse {
	layers, f := chain(args.Principal, args.Scope)
	if f == nil {
		f = check(args.ServiceArgs, "config.write", args.Section, layers)
	}
	if f != nil {
		return configservice.ResetResponse{Common: *args.Fail(f.status, f.err)}
	}
	sec, f := s.section(args.Section)
	if f != nil {
		return configservice.ResetResponse{Common: *args.Fail(f.status, f.err)}
	}
	scope := layers[len(layers)-1].key
	row, overrides, f := s.load(sec, scope)
	if f == nil && row != nil {
		next := map[string]any{}
		if len(args.Keys) > 0 {
			next = config.Clone(overrides).(map[string]any)
			for _, k := range args.Keys {
				config.Delete(next, k)
			}
		}
		// Dropping a secret is changing it
		sec.schema.Leaves(func(path string, leaf *config.Schema) {
			_, had := config.Get(overrides, path)
			_, kept := config.Get(next, path)
			if f == nil && leaf.Type == "secret" && had && !kept {
				f = checkSecrets(args.ServiceArgs, sec.ID, layers)
			}
		})
		if f == nil {
			if row, f = s.store(args.ServiceArgs, sec, scope, row, overrides, next); f == nil {
				s.changed(sec.ID, scope)
			}
		}
	}
	if f != nil {
		return configservice.ResetResponse{Common: *args.Fail(f.status, f.err)}
	}
	value, sources, row, f := s.resolved(sec, layers, nil)
	if f != nil {
		return configservice.ResetResponse{Common: *args.Fail(f.status, f.err)}
	}
	return configservice.ResetResponse{Common: features.OK(), View: s.view(sec, args.Scope, value, sources, row)}
}
