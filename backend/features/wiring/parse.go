package wiring

import (
	foundation "github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/services/filesservice"
	filesimpl "github.com/SgtFlame/foundation/backend/features/records/services/filesservice/impl"
	filesproxy "github.com/SgtFlame/foundation/backend/features/records/services/filesservice/proxy"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice"
	parseimpl "github.com/SgtFlame/foundation/backend/features/text/services/parseservice/impl"
	parseproxy "github.com/SgtFlame/foundation/backend/features/text/services/parseservice/proxy"
)

// Parse (forge 40c): the parse service (CSV first), its access type and
// Parse CSV's dependencies (the node itself is a record node). The service comes from the locator, so serve --services can
// run it in another process; it reads uploaded files through the files
// service.
func init() {
	Register(Feature{
		Name: "parse",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			parseservice.ContributeAccess(reg)
			recordsnodes.SetParse(func() recordsnodes.ParseDeps {
				return recordsnodes.ParseDeps{
					Parse: parseService(env),
					MaxRows: func(args foundation.ServiceArgs) int {
						return env.RecordDeps().SettingsFor(args).ImportMaxRows
					},
				}
			})
			return nil
		},
		Services: func(env *Env) { parseService(env) },
	})
}

// parseService is the parse service as the locator gives it (made once).
func parseService(env *Env) parseservice.Service {
	loc := env.Services()
	return parseproxy.Use(loc, func() parseservice.Service {
		return parseimpl.CreateService(parseimpl.WithFiles(func() filesservice.Service {
			// The files service serve made (the locator gives the same one)
			return filesproxy.Use(loc, func() filesservice.Service { return filesimpl.CreateService() })
		}))
	})
}
