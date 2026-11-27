package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/cognition/traces"
)

// onlyServices refuses a principal that is not a service's: traces are
// kept for engines in other processes, not for people.
func onlyServices(args features.ServiceArgs) *features.ServiceResponse {
	if args.Principal.Kind != features.PrincipalService {
		return args.Fail(http.StatusForbidden, log.Code("services_only").New("only services may keep traces this way"))
	}
	return nil
}

func (s *service) store(args features.ServiceArgs) (*traces.Store, *features.ServiceResponse) {
	if f := onlyServices(args); f != nil {
		return nil, f
	}
	st := traces.In(s.db)
	if st == nil {
		return nil, args.Fail(http.StatusServiceUnavailable, log.Code("traces_unavailable").New("traces are not kept here"))
	}
	return st, nil
}

func (s *service) SaveRun(args cognitionservice.SaveRunArgs) cognitionservice.SaveRunResponse {
	st, f := s.store(args.ServiceArgs)
	if f != nil {
		return cognitionservice.SaveRunResponse{Common: *f}
	}
	rec := args.Run
	if rec.ID == 0 {
		return cognitionservice.SaveRunResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("run_without_id").New("a run is kept with its id"))}
	}
	tenant := args.Principal.TenantID
	rec.TenantID = tenant
	var err error
	if args.Begin {
		err = st.Begin(args.Context(), rec)
	} else {
		steps := args.Steps
		for i := range steps {
			steps[i].TenantID, steps[i].RunID = tenant, rec.ID
		}
		err = st.Save(args.Context(), rec, steps)
	}
	if err != nil {
		return cognitionservice.SaveRunResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return cognitionservice.SaveRunResponse{Common: features.OK()}
}

func (s *service) ReadRun(args cognitionservice.ReadRunArgs) cognitionservice.ReadRunResponse {
	st, f := s.store(args.ServiceArgs)
	if f != nil {
		return cognitionservice.ReadRunResponse{Common: *f}
	}
	run, steps, found, err := st.Get(args.Context(), args.Principal.TenantID, args.RunID)
	if err != nil {
		return cognitionservice.ReadRunResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return cognitionservice.ReadRunResponse{Common: features.OK(), Found: found, Run: run, Steps: steps}
}

func (s *service) PurgeRuns(args cognitionservice.PurgeRunsArgs) cognitionservice.PurgeRunsResponse {
	st, f := s.store(args.ServiceArgs)
	if f != nil {
		return cognitionservice.PurgeRunsResponse{Common: *f}
	}
	if err := st.Purge(args.Context(), args.KeepRuns, args.KeepDays); err != nil {
		return cognitionservice.PurgeRunsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return cognitionservice.PurgeRunsResponse{Common: features.OK()}
}
