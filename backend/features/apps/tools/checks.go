package apptools

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// AppChecks runs every check of other apps the caller has (forge 19f: an
// installed app's manifest tool with checks) for one app, as the caller,
// and gives their warnings, each path prefixed with the tool's id. A check
// that fails or refuses the caller adds nothing: checks never stop
// forge.check.
func AppChecks(call tools.Call, reg *extensions.Registry, app, branch string) []live.Problem {
	out := []live.Problem{}
	all := tools.Available(reg, call.ServiceArgs)
	slices.SortFunc(all, func(a, b tools.Tool) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	args, _ := json.Marshal(map[string]string{"appId": app, "branch": branch})
	for _, t := range all {
		if !t.Checks || t.Run == nil {
			continue
		}
		c := call
		c.Args = args
		res, err := t.Run(c)
		if err != nil {
			continue
		}
		for _, w := range warningsOf(res.Content["warnings"]) {
			path := t.ID
			if w.Path != "" {
				path += ":" + w.Path
			}
			out = append(out, live.Problem{Code: "warning", Path: path, Message: w.Message, Fix: w.Fix})
		}
	}
	return out
}

// warningsOf reads a check's warnings output, a list of {path, message, fix}.
func warningsOf(v any) []live.Problem {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var ws []struct {
		Path    any    `json:"path"`
		Message string `json:"message"`
		Fix     string `json:"fix"`
	}
	if json.Unmarshal(raw, &ws) != nil {
		return nil
	}
	var out []live.Problem
	for _, w := range ws {
		if w.Message == "" {
			continue
		}
		path := ""
		if w.Path != nil {
			path = fmt.Sprint(w.Path)
		}
		out = append(out, live.Problem{Path: path, Message: w.Message, Fix: w.Fix})
	}
	return out
}
