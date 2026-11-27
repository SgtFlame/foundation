package branches

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Approval kinds (forge 19d): what an approver of an app approves. A
// commit touches the kinds of the parts and manifest items it changed.
const (
	KindEntities = "entities"
	KindPages    = "pages"
	KindAccess   = "access"
	KindGraphs   = "graphs"
)

// Kinds are the approval kinds in order, with the action approving each;
// app.approve approves them all.
var Kinds = []struct{ Kind, Action string }{
	{KindEntities, "app.approveEntities"},
	{KindPages, "app.approvePages"},
	{KindAccess, "app.approveAccess"},
	{KindGraphs, "app.approveGraphs"},
}

// SemanticDiffGraph is the built-in graph a commit's semantic diff job
// runs (forge 19d), when one is registered: On call {commit, app,
// message, structured}, ending with app.explain.
const SemanticDiffGraph = "app-semanticDiff"

// KindsOf are the approval kinds a commit touches, from its structured
// diff (or its parts' types when it has none).
func KindsOf(c Commit) []string {
	var pd []PartDiff
	_ = json.Unmarshal(c.Structured, &pd)
	seen := map[string]bool{}
	byType := map[string]string{"entity": KindEntities, "prefab": KindPages, "graph": KindGraphs, "app": KindPages}
	if len(pd) == 0 {
		for _, p := range c.Parts {
			seen[byType[p.Type]] = true
		}
	}
	for _, d := range pd {
		root := d.Type + ":" + d.ID
		for _, ch := range d.Changes {
			rest := strings.TrimPrefix(strings.TrimPrefix(ch.Path, root), ".")
			field := rest
			if i := strings.IndexAny(field, ".["); i >= 0 {
				field = field[:i]
			}
			switch {
			case rest == "":
				seen[byType[d.Type]] = true
			case d.Type == "entity" && field == "access":
				seen[KindAccess] = true
			case d.Type == "app":
				switch field {
				case "roles", "settingsSchema", "settings", "ephemeral":
					seen[KindAccess] = true
				case "commands", "queries", "triggers", "tools", "connectors", "projectors":
					seen[KindGraphs] = true
				case "entities":
					seen[KindEntities] = true
				default:
					seen[KindPages] = true
				}
			default:
				seen[byType[d.Type]] = true
			}
		}
	}
	var out []string
	for _, k := range Kinds {
		if seen[k.Kind] {
			out = append(out, k.Kind)
		}
	}
	return out
}

// ApprovalStatus is how far a commit is approved: approved when every
// kind it touches is, else the kinds still waiting.
func ApprovalStatus(c Commit) (approved bool, waiting []string) {
	got := map[string]bool{}
	for _, a := range c.Approvals {
		for _, k := range a.Kinds {
			got[k] = true
		}
	}
	for _, k := range KindsOf(c) {
		if !got[k] {
			waiting = append(waiting, k)
		}
	}
	return len(c.Approvals) > 0 && len(waiting) == 0, waiting
}

// ApproverKinds are the kinds a principal approves in an app.
func ApproverKinds(args features.ServiceArgs, app string) []string {
	res := apps.Resource(args.Principal, app)
	if authz.Check(args.Context(), args.Principal, "app.approve", res).Allowed {
		out := make([]string, len(Kinds))
		for i, k := range Kinds {
			out[i] = k.Kind
		}
		return out
	}
	var out []string
	for _, k := range Kinds {
		if authz.Check(args.Context(), args.Principal, k.Action, res).Allowed {
			out = append(out, k.Kind)
		}
	}
	return out
}

// contributeReview registers the commands on a commit: app.approve and
// app.explain.
func contributeReview(reg *extensions.Registry) {
	commitCommand := func(c live.Command, decide func(d live.Decision, cm Commit, app string) ([]live.NewEvent, []live.Problem)) {
		c.Area, c.TargetType, c.Projection, c.NoBranch = live.AreaDefinitions, CommitType, CommitProjector, true
		c.NewTargetFor = func(_ features.ServiceArgs, raw json.RawMessage) string {
			var a struct{ Commit string }
			_ = json.Unmarshal(raw, &a)
			return a.Commit
		}
		c.Resource = func(p features.Principal, t live.Target) string { return apps.Resource(p, AppOfCommit(t.ID)) }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var a struct{ Commit string }
			_ = json.Unmarshal(d.Args, &a)
			if a.Commit != "" && a.Commit != d.Target.ID {
				return nil, problem("", "target", fmt.Sprintf("the target is %s, not the commit %s", d.Target.ID, a.Commit), "leave the target out")
			}
			var cm Commit
			if d.State == nil || json.Unmarshal(d.State, &cm) != nil || cm.ID == "" {
				return nil, problem("app_commit_not_found", "commit", fmt.Sprintf("there is no commit %q", d.Target.ID), "")
			}
			events, problems := decide(d, cm, cm.App)
			if problems != nil {
				return nil, problems
			}
			d.NoUndo(cm.App, "approvals and diffs are kept for the history")
			return events, nil
		}
		live.Commands.Register(reg, c)
	}
	commitCommand(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.approve", Source: "apps"}, Title: "Approve an app commit",
		Description: "Approves a commit of an app (forge 19d) for the part kinds the sender approves (app.approve: all; app.approveEntities, approvePages, approveAccess, approveGraphs: one each) among those the commit touches: entities, pages (prefabs, pages, nav, editor kinds), access (roles, settings, entity access rules) and graphs (blueprints, commands, queries, triggers, tools). The commit is approved once every kind it touches is; read the commit's semantic and structured diffs first (app.history, forge.approvals).",
		Args:        json.RawMessage(`{"type":"object","properties":{"commit":{"type":"string","description":"The commit's id, <app>-c<n>"},"comment":{"type":"string","description":"What the approver says about it"}},"required":["commit"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"commit":"tasks-c123","comment":"The refund limit reads well"}`)},
		Events:      []string{EventApproved}, Action: "app.read",
	}, func(d live.Decision, cm Commit, app string) ([]live.NewEvent, []live.Problem) {
		var a struct{ Comment string }
		_ = json.Unmarshal(d.Args, &a)
		touched := KindsOf(cm)
		mine := ApproverKinds(d.ServiceArgs, app)
		var kinds []string
		for _, k := range touched {
			if slices.Contains(mine, k) {
				kinds = append(kinds, k)
			}
		}
		if len(kinds) == 0 {
			return nil, problem("app_not_approver", "commit", fmt.Sprintf("you approve none of what the commit changes (%s)", strings.Join(touched, ", ")), "ask an approver of the app")
		}
		done := map[string]bool{}
		for _, ap := range cm.Approvals {
			if ap.By == d.Principal.UserID {
				for _, k := range ap.Kinds {
					done[k] = true
				}
			}
		}
		if !slices.ContainsFunc(kinds, func(k string) bool { return !done[k] }) {
			return nil, problem("already_approved", "commit", "you approved this commit already", "")
		}
		return []live.NewEvent{{Type: EventApproved, Data: Approval{Kinds: kinds, Comment: strings.TrimSpace(a.Comment)}}}, nil
	})
	commitCommand(live.Command{
		Descriptor: extensions.Descriptor{ID: "app.explain", Source: "apps"}, Title: "Set a commit's semantic diff",
		Description: "Sets a commit's semantic diff once (forge 19d): what its changes mean, {summary, changes: [{kind, part, path, summary}]}, or the error that kept it from being described, {error}, which a later explain may replace. Sent by the app-semanticDiff graph a commit's job runs.",
		Args:        json.RawMessage(`{"type":"object","properties":{"commit":{"type":"string","description":"The commit's id"},"semantic":{"type":"object","description":"{summary, changes: [{kind (added, changed, removed, moved), part (type:id), path, summary}]}, or {error}"}},"required":["commit","semantic"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"commit":"tasks-c123","semantic":{"summary":"Orders get a refund limit.","changes":[{"kind":"added","part":"entity:order","path":"entity:order.fields.limit","summary":"Orders hold a refund limit."}]}}`)},
		Events:      []string{EventExplained}, Action: "app.write",
	}, func(d live.Decision, cm Commit, _ string) ([]live.NewEvent, []live.Problem) {
		var a struct{ Semantic map[string]any }
		_ = json.Unmarshal(d.Args, &a)
		if len(a.Semantic) == 0 {
			return nil, problem("", "semantic", "a semantic diff is needed", "")
		}
		var prev map[string]any
		_ = json.Unmarshal(cm.Semantic, &prev)
		if len(prev) > 0 && prev["error"] == nil {
			return nil, problem("semantic_set", "commit", "the commit's semantic diff is set already", "")
		}
		return []live.NewEvent{{Type: EventExplained, Data: map[string]any{"semantic": a.Semantic}}}, nil
	})
}
