// Package toolsbuiltin contributes the tools no feature owns: time.now.
package toolsbuiltin

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Contribute registers the built-in tools.
func Contribute(reg *extensions.Registry) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "time.now", Order: 900, Source: "tools"},
		Title:       "Current time",
		Description: "Returns the current date and time, in UTC and in a time zone: the server's, or the IANA zone given (such as Europe/Paris). Use it for anything that depends on today's date.",
		Input:       json.RawMessage(`{"type":"object","properties":{"timeZone":{"type":"string","description":"An IANA time zone, e.g. America/New_York"}}}`),
		Effect:      tools.EffectNone,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct{ TimeZone string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			now := time.Now()
			loc := time.Local
			if args.TimeZone != "" {
				if loc, err = time.LoadLocation(args.TimeZone); err != nil {
					return tools.Result{}, fmt.Errorf("%q is not a known time zone", args.TimeZone)
				}
			}
			local := now.In(loc)
			zone, _ := local.Zone()
			return tools.Result{Content: map[string]any{
				"utc": now.UTC().Format(time.RFC3339), "local": local.Format(time.RFC3339), "timeZone": loc.String(), "abbreviation": zone,
				"weekday": local.Weekday().String(),
			}}, nil
		},
	})
}
