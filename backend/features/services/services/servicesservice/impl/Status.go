package impl

import (
	"net/http"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/services/services/servicesservice"
)

var log = logging.In("services")

// Status lists the processes announced now and who serves each service.
func (s *service) Status(args servicesservice.StatusArgs) servicesservice.StatusResponse {
	const action, resource = "services.read", "app"
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return servicesservice.StatusResponse{Common: *args.Fail(authz.Status, err)}
	}
	var procs []servicesservice.Process
	if s.list != nil {
		var err error
		if procs, err = s.list(args.Context()); err != nil {
			return servicesservice.StatusResponse{Common: *args.Fail(http.StatusServiceUnavailable, log.Wrap(err))}
		}
	}
	by := map[string][]string{}
	for _, n := range s.known {
		by[n] = nil
	}
	for _, p := range procs {
		if p.Draining {
			continue
		}
		for _, svc := range p.Services {
			by[svc] = append(by[svc], p.Process)
		}
	}
	out := servicesservice.StatusResponse{Common: features.OK(), Processes: procs}
	for _, n := range slices.Sorted(func(yield func(string) bool) {
		for k := range by {
			if !yield(k) {
				return
			}
		}
	}) {
		out.Services = append(out.Services, servicesservice.ServiceStatus{Name: n, Processes: by[n]})
	}
	return out
}
