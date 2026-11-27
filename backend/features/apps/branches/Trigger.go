package branches

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

// OnMerged is On app branch merged (forge 19f), a trigger event: a
// trigger starts each time a branch of any app in the tenant is merged
// into another and its merge commit is made. It gives only the app's id,
// the two branches and the commit, so an app keeping its own records per
// app branch (the Requirements app's serves links) can carry them into
// the target.
const OnMerged = "apps.onBranchMerged"

// ContributeTrigger registers On app branch merged: the node and its
// trigger event, on the def stream's branch merged events.
func ContributeTrigger(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: OnMerged, Source: "apps", Order: 640}, Title: "On app branch merged", Category: "Events", Event: true,
		Kinds:       []string{livenodes.KindTrigger},
		Description: "Starts a trigger, in the background as the app's own principal, each time a branch of any app in the tenant is merged into another (forge 19e's merge commit made): for an app that keeps records per app branch, such as the Requirements app's links from parts to board sections, to carry the source branch's into the target",
		Example:     "On app branch merged, copying the source branch's serves links to the target branch",
		Tags:        []string{"app", "branch", "merge", "trigger"},
		Config:      json.RawMessage(`{"type":"object","properties":{}}`),
		Pins: cognition.Pins{Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
			cognition.DataPin("app", "App", "string", "The merged app's id"),
			cognition.DataPin("branch", "Branch", "string", "The branch merged from"),
			cognition.DataPin("into", "Into", "string", "The branch merged into, main mostly"),
			cognition.DataPin("commit", "Commit", "string", "The merge commit's id")}},
	})
	livenodes.TriggerEvents.Register(reg, livenodes.TriggerEvent{
		Descriptor: extensions.Descriptor{ID: OnMerged, Source: "apps"},
		Subjects: func(args features.ServiceArgs, _ string, _ json.RawMessage) (live.Area, []string, error) {
			return live.AreaDefinitions, livenodes.EventSubjects(args.Principal.TenantID, live.AreaDefinitions, TargetType, []string{EventMerged}), nil
		},
		Inputs: func(_ features.ServiceArgs, _ json.RawMessage, e live.Event) (cognition.Values, error) {
			var d struct{ App, Branch, Into, Commit string }
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil, fmt.Errorf("a merged event cannot be read: %v", err)
			}
			return cognition.Values{"app": d.App, "branch": d.Branch, "into": d.Into, "commit": d.Commit}, nil
		},
	})
}
