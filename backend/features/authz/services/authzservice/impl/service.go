package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("authz")

type service struct {
	pol    *authz.Policy
	member func(tenant, user uint64) (bool, error)
}

// check runs authz.Check for action on resource.
func check(args features.ServiceArgs, action, resource string) *features.ServiceResponse {
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// inTenant tells whether resource is the application or within the
// principal's tenant: the only places a principal administers from.
func inTenant(p features.Principal, resource string) bool {
	t := authz.TenantOf(resource)
	return t == 0 && authz.Normalize(resource) == authz.OnApp || t == p.TenantID
}

// outside is the 404 of a resource in another tenant, which names nothing.
func outside(args features.ServiceArgs, resource string) *features.ServiceResponse {
	return args.Fail(http.StatusNotFound, log.Code("resource_not_found").With("resource", resource).New("the resource is not in your tenant"))
}

func internal(args features.ServiceArgs, err error) *features.ServiceResponse {
	return args.Fail(http.StatusInternalServerError, log.Wrap(err))
}
