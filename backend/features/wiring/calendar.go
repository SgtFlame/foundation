package wiring

import (
	"github.com/SgtFlame/foundation/backend/features/calendar"
	"github.com/SgtFlame/foundation/backend/features/calendar/caltest"
	"github.com/SgtFlame/foundation/backend/features/calendar/ical"
	calendarnodes "github.com/SgtFlame/foundation/backend/features/calendar/nodes"
	calendarsettings "github.com/SgtFlame/foundation/backend/features/calendar/settings"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Calendar (forge 28): the calendar section, the time and iCalendar nodes
// (.ics parsed in a limited child process), calendar.events@1 and the
// Calendar test server with its control call.
func init() {
	Register(Feature{
		Name: "calendar",
		Contribute: func(reg *extensions.Registry, env *Env) error {
			calendarsettings.Contribute(reg)
			calendarnodes.Contribute(reg)
			calendarnodes.ContributeIcal(reg, func() calendarnodes.IcalDeps {
				d := calendarnodes.IcalDeps{}
				if exe := env.Executable(); exe != "" {
					d.Read = ical.Child(exe)
				}
				if files := env.RecordFiles(); files != nil {
					d.Open = files.Open
				}
				return d
			})
			calendar.Contribute(reg)
			caltest.Contribute(reg)
			caltest.ContributeLive(reg, func() connections.Deps { return env.Connections() }) // caltest.call, for forge caltest
			return nil
		},
		Adapter: func(o *adapter.Options) {
			o.Types = append(o.Types, caltest.Type())
			o.Contracts = append(o.Contracts, calendar.EventsContract(), caltest.ControlContract())
			o.Impls = append(o.Impls, caltest.Default.Impl())
		},
	})
}
