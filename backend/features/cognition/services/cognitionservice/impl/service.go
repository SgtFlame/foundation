package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db     *gorm.DB
	engine *engine.Engine
}

var log = logging.In("cognition")

// check runs authz.Check for action on a resource under the tenant's
// Cognition: "graph:<id>", "runs", ….
func check(args features.ServiceArgs, action, resource string) *features.ServiceResponse {
	res := authz.Tenant(args.Principal) + "/cognition/" + resource
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// info is a graph's summary; every graph is built in for now.
func info(g cognition.Graph) cognitionservice.GraphInfo {
	return cognitionservice.GraphInfo{ID: g.ID, Name: g.Name, Description: g.Description, Kind: g.Kind, Version: g.Version, BuiltIn: true}
}
