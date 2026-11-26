package branches

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The app's draft projection and target type (apps/manifests, which this
// package does not import).
const (
	appProjector = "apps.app"
	appType      = "app"
)

func problem(code, path, message, fix string) []live.Problem {
	return []live.Problem{{Code: code, Path: path, Message: message, Fix: fix}}
}

// branchTarget is a branch record's target.
func branchTarget(app, name string) live.Target {
	return live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: KeyOf(app, name)}
}

// newCommitID is a new commit's id: <app>-c<tsid>.
func newCommitID(app string) string { return fmt.Sprintf("%s-c%d", app, features.NewTSID()) }

// command is one branch command's decision.
type command struct {
	live.Decision
	r   *reader
	rec Record
	has bool
}

// read reads a target's projected value through the decision.
func (c *command) read(projector string, t live.Target, v any) (bool, error) {
	raw, err := c.Read(projector, t)
	if err != nil || raw == nil {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}

// startMain makes main's record and first commit when it has none: the
// app's drafts as they are now. It returns main's head and the events.
func (c *command) startMain(app string) (string, []live.NewEvent, []live.Problem) {
	var mainRec Record
	has, err := c.read(Projector, branchTarget(app, live.Main), &mainRec)
	if err != nil {
		return "", nil, problem("", "", err.Error(), "")
	}
	if has && mainRec.Head != "" {
		return mainRec.Head, nil, nil
	}
	var draft json.RawMessage
	if ok, err := c.read(appProjector, live.Target{Area: live.AreaDefinitions, Type: appType, ID: app}, &draft); err != nil || !ok {
		return "", nil, problem("app_not_found", "app", fmt.Sprintf("there is no app %q", app), "make it first with app.create {name}")
	}
	parts, err := c.r.Changes(c.Context(), c.Principal.TenantID, live.Branch{App: app}, "")
	if err != nil {
		return "", nil, problem("", "", err.Error(), "")
	}
	id := newCommitID(app)
	mt := branchTarget(app, live.Main)
	ct := live.Target{Area: live.AreaDefinitions, Type: CommitType, ID: id}
	var events []live.NewEvent
	if !has {
		events = append(events, live.NewEvent{Type: EventCreated, Target: &mt, Data: Record{App: app, Name: live.Main, Status: StatusOpen, Origin: OriginManual}})
	}
	events = append(events,
		live.NewEvent{Type: EventCreated, Target: &ct, Data: Commit{ID: id, App: app, Branch: live.Main, Parts: parts, Message: "The app as it was when branching began"}},
		live.NewEvent{Type: EventCommitted, Target: &mt, Data: map[string]string{"commit": id}})
	return id, events, nil
}

// CreateArgs are app.branch.create's arguments.
type CreateArgs struct {
	App          string `json:"app"`
	Name         string `json:"name"`
	From         string `json:"from,omitempty"` // a branch; main when left out
	Title        string `json:"title,omitempty"`
	Origin       string `json:"origin,omitempty"`
	Requirements string `json:"requirements,omitempty"`
}

// CommitArgs are app.commit's arguments.
type CommitArgs struct {
	App          string `json:"app"`
	Branch       string `json:"branch,omitempty"` // main when left out
	Message      string `json:"message"`
	ExpectedHead string `json:"expectedHead,omitempty"`
}

// DiscardArgs are app.discard's arguments.
type DiscardArgs struct {
	App    string `json:"app"`
	Branch string `json:"branch,omitempty"`
	Type   string `json:"type"`
	ID     string `json:"id"`
}

type spec struct {
	id, title, description, args, example string
	events                                []string
	target                                func(raw json.RawMessage) string
	decide                                func(c *command) ([]live.NewEvent, []live.Problem)
}

// appBranchOf reads app and branch from arguments.
func appBranchOf(raw json.RawMessage) (string, string) {
	var a struct {
		App, Branch, Name string
	}
	_ = json.Unmarshal(raw, &a)
	name := a.Branch
	if name == "" {
		name = a.Name
	}
	return a.App, name
}

func specs() []spec {
	return []spec{{
		id: "app.branch.create", title: "Make an app branch", events: []string{EventCreated, EventCommitted},
		description: "Makes a branch of an app: a named overlay of drafts of the parts the app owns, starting as another branch's (main by default) last commit. Send app.* , prefab.*, bp.* and entity.* commands with the request's branch to change parts there, while main stays as it is; nothing is published from a branch. Making the first branch of an app commits main as it is.",
		args:        `{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"name":{"type":"string","description":"The branch's name: lower-case letters, digits and -, such as refund-limits"},"from":{"type":"string","description":"The branch it starts from, at its last commit; main when left out"},"title":{"type":"string","description":"What the branch is for, in a few words"},"origin":{"type":"string","enum":["manual","builder","requirements","merge"],"description":"Who made it; manual when left out"},"requirements":{"type":"string","description":"The requirements branch it answers, <space>:<branch>"}},"required":["app","name"]}`,
		example:     `{"app":"tasks","name":"refund-limits","title":"Refund limits"}`,
		target: func(raw json.RawMessage) string {
			app, name := appBranchOf(raw)
			return KeyOf(app, name)
		},
		decide: func(c *command) ([]live.NewEvent, []live.Problem) {
			var a CreateArgs
			_ = json.Unmarshal(c.Args, &a)
			if !live.ValidBranchName(a.Name) {
				return nil, problem("", "name", fmt.Sprintf("%q is not a branch name", a.Name), "use lower-case letters, digits and -, not main")
			}
			if c.has {
				return nil, problem("branch_exists", "name", fmt.Sprintf("the app %s has a branch %q already", a.App, a.Name), "choose another name")
			}
			from := a.From
			if from == "" {
				from = live.Main
			}
			var head string
			var events []live.NewEvent
			if from == live.Main {
				var problems []live.Problem
				if head, events, problems = c.startMain(a.App); problems != nil {
					return nil, problems
				}
			} else {
				var base Record
				if ok, err := c.read(Projector, branchTarget(a.App, from), &base); err != nil || !ok {
					return nil, problem("branch_not_found", "from", fmt.Sprintf("the app %s has no branch %q", a.App, from), "")
				}
				head = base.Head
			}
			origin := a.Origin
			if origin == "" {
				origin = OriginManual
			}
			rec := Record{App: a.App, Name: a.Name, Title: a.Title, Base: Base{Branch: from, Commit: head}, Status: StatusOpen, Origin: origin, Requirements: a.Requirements, Head: head}
			return append(events, live.NewEvent{Type: EventCreated, Data: rec}), nil
		},
	}, {
		id: "app.branch.abandon", title: "Abandon an app branch", events: []string{EventAbandoned},
		description: "Abandons a branch: it can no longer be changed, and is kept, with its commits, for the history.",
		args:        `{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"branch":{"type":"string","description":"The branch's name"}},"required":["app","branch"]}`,
		example:     `{"app":"tasks","branch":"refund-limits"}`,
		target: func(raw json.RawMessage) string {
			app, name := appBranchOf(raw)
			return KeyOf(app, name)
		},
		decide: func(c *command) ([]live.NewEvent, []live.Problem) {
			switch {
			case !c.has:
				return nil, problem("branch_not_found", "branch", "there is no such branch", "")
			case c.rec.Name == live.Main:
				return nil, problem("", "branch", "main is never abandoned", "")
			case c.rec.Status != StatusOpen:
				return nil, problem("branch_closed", "branch", fmt.Sprintf("the branch is %s", c.rec.Status), "")
			}
			return []live.NewEvent{{Type: EventAbandoned, Data: map[string]any{}}}, nil
		},
	}, {
		id: "app.commit", title: "Commit an app branch", events: []string{EventCreated, EventCommitted},
		description: "Commits what a branch (main by default) changed since its last commit: an immutable record of those parts as they are now, linked to the last commit, which becomes the branch's head. expectedHead is the head the sender saw: when someone committed since, it is refused with app_head_moved, and the sender reviews and commits again.",
		args:        `{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"branch":{"type":"string","description":"The branch; main when left out"},"message":{"type":"string","description":"What the commit changes, in a sentence"},"expectedHead":{"type":"string","description":"The head commit the sender saw (app.branches)"}},"required":["app","message"]}`,
		example:     `{"app":"tasks","branch":"refund-limits","message":"Adds a refund limit to orders"}`,
		target: func(raw json.RawMessage) string {
			app, name := appBranchOf(raw)
			return KeyOf(app, name)
		},
		decide: func(c *command) ([]live.NewEvent, []live.Problem) {
			var a CommitArgs
			_ = json.Unmarshal(c.Args, &a)
			name := a.Branch
			if name == "" {
				name = live.Main
			}
			if strings.TrimSpace(a.Message) == "" {
				return nil, problem("", "message", "a commit needs a message", "say in a sentence what it changes")
			}
			var events []live.NewEvent
			rec := c.rec
			if !c.has {
				if name != live.Main {
					return nil, problem("branch_not_found", "branch", fmt.Sprintf("the app %s has no branch %q", a.App, name), "")
				}
				var draft json.RawMessage
				if ok, err := c.read(appProjector, live.Target{Area: live.AreaDefinitions, Type: appType, ID: a.App}, &draft); err != nil || !ok {
					return nil, problem("app_not_found", "app", fmt.Sprintf("there is no app %q", a.App), "")
				}
				rec = Record{App: a.App, Name: live.Main, Status: StatusOpen, Origin: OriginManual}
				events = append(events, live.NewEvent{Type: EventCreated, Data: rec})
			}
			if rec.Status != StatusOpen {
				return nil, problem("branch_closed", "branch", fmt.Sprintf("the branch is %s", rec.Status), "")
			}
			if a.ExpectedHead != "" && a.ExpectedHead != rec.Head {
				return nil, problem("app_head_moved", "expectedHead", fmt.Sprintf("the branch's head is %s now, not %s: someone committed since", rec.Head, a.ExpectedHead), "review what changed and commit again with the new head")
			}
			parts, err := c.r.Changes(c.Context(), c.Principal.TenantID, live.Branch{App: a.App, Name: name}, rec.Head)
			if err != nil {
				return nil, problem("", "", err.Error(), "")
			}
			if len(parts) == 0 {
				return nil, problem("nothing_to_commit", "branch", "the branch has no changes since its last commit", "")
			}
			id := newCommitID(a.App)
			ct := live.Target{Area: live.AreaDefinitions, Type: CommitType, ID: id}
			return append(events,
				live.NewEvent{Type: EventCreated, Target: &ct, Data: Commit{ID: id, App: a.App, Branch: name, Parent: rec.Head, Parts: parts, Message: strings.TrimSpace(a.Message)}},
				live.NewEvent{Type: EventCommitted, Data: map[string]string{"commit": id}}), nil
		},
	}, {
		id: "app.discard", title: "Discard a part's changes", events: []string{live.EventBranched},
		description: "Discards what a branch (main by default) changed in one part since its last commit: the part goes back to its state at the head, or, on a branch, to its base's when the branch never committed it, and a part made since is removed.",
		args:        `{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"branch":{"type":"string","description":"The branch; main when left out"},"type":{"type":"string","enum":["app","prefab","graph","entity"],"description":"The part's type"},"id":{"type":"string","description":"The part's id"}},"required":["app","type","id"]}`,
		example:     `{"app":"tasks","branch":"refund-limits","type":"prefab","id":"orderRow"}`,
		target: func(raw json.RawMessage) string {
			app, name := appBranchOf(raw)
			return KeyOf(app, name)
		},
		decide: func(c *command) ([]live.NewEvent, []live.Problem) {
			var a DiscardArgs
			_ = json.Unmarshal(c.Args, &a)
			name := a.Branch
			if name == "" {
				name = live.Main
			}
			if !c.has || c.rec.Head == "" {
				return nil, problem("nothing_to_discard", "branch", "the branch has no commit to go back to", "commit it first")
			}
			if c.rec.Status != StatusOpen {
				return nil, problem("branch_closed", "branch", fmt.Sprintf("the branch is %s", c.rec.Status), "")
			}
			if !live.ValidToken(a.ID) {
				return nil, problem("", "id", fmt.Sprintf("%q is not a part id", a.ID), "")
			}
			b := live.Branch{App: a.App, Name: name}
			part := live.Target{Area: live.AreaDefinitions, Type: a.Type, ID: a.ID}
			draft := part.OnBranch(b)
			ref, ok, err := c.r.refAt(c.Context(), c.Principal.TenantID, c.rec.Head, a.Type, a.ID)
			if err != nil {
				return nil, problem("", "", err.Error(), "")
			}
			data := live.Branched{}
			if ok && !ref.Removed && ref.Target == draft.ID {
				v, err := c.r.at(c.Context(), c.Principal.TenantID, live.Target{Area: live.AreaDefinitions, Type: a.Type, ID: ref.Target}, ref.Seq)
				if err != nil {
					return nil, problem("", "", err.Error(), "")
				}
				data = live.Branched{From: ref.Seq, State: v}
			} else if b.IsMain() && !ok {
				return nil, problem("", "id", fmt.Sprintf("%s %s was made since the last commit", a.Type, a.ID), "delete it with its own command")
			}
			return []live.NewEvent{{Type: live.EventBranched, Target: &draft, Data: data}}, nil
		},
	}}
}

// Contribute registers the branches' projectors, commands, queries and
// the live.branching hook; deps returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	r := newReader(reg, deps)
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: Projector, Source: "apps"}, Title: "App branches",
		Description: "Each app's branches, keyed appBranch.<app>_<name> (main once committed): {app, name, title, base: {branch, commit}, status, origin, requirements, head, commits, mergedInto, createdBy, createdAt, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: foldBranch,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: CommitProjector, Source: "apps"}, Title: "App commits",
		Description: "Every commit of an app's branches, keyed appCommit.<app>-c<n>: {id, app, branch, parent, mergeParent, parts: [{type, id, target, seq, removed}], message, author, createdAt, structured, semantic, approvals}",
		Area:        live.AreaDefinitions, Types: []string{CommitType}, Fold: foldCommit,
	})
	live.Branchings.Register(reg, live.Branching{Descriptor: extensions.Descriptor{ID: "apps", Source: "apps"}, Base: r.Base, Check: r.Check})
	for _, s := range specs() {
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "apps"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: Projector, Action: "app.write", NoBranch: true,
			NewTargetFor: func(_ features.ServiceArgs, raw json.RawMessage) string { return s.target(raw) },
			Resource: func(p features.Principal, t live.Target) string {
				app, _ := SplitKey(t.ID)
				return apps.Resource(p, app)
			},
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				app, name := appBranchOf(d.Args)
				if !live.ValidToken(app) {
					return nil, problem("", "app", fmt.Sprintf("%q is not an app id", app), "")
				}
				if name == "" {
					name = live.Main
				}
				if d.Target.ID != KeyOf(app, name) {
					return nil, problem("", "target", fmt.Sprintf("the target is %s, not the branch %s of %s", d.Target.ID, name, app), "leave the target out")
				}
				c := &command{Decision: d, r: r}
				if d.State != nil {
					if err := json.Unmarshal(d.State, &c.rec); err != nil {
						return nil, problem("", "", err.Error(), "")
					}
					c.has = true
				}
				events, problems := s.decide(c)
				if problems != nil {
					return nil, problems
				}
				d.NoUndo(app, "branches and commits are kept for the history")
				return events, nil
			},
		})
	}
	contributeQueries(reg, r)
}

// PartAt is a part's state at a commit, for the features that publish,
// diff and merge commits.
func PartAt(reg *extensions.Registry, deps func() Deps) func(args features.ServiceArgs, commit string, t live.Target) (live.Projected, bool, error) {
	r := newReader(reg, deps)
	return func(args features.ServiceArgs, commit string, t live.Target) (live.Projected, bool, error) {
		return r.PartAt(args.Context(), args.Principal.TenantID, commit, t)
	}
}

// ChangesOf is what a branch changed since its head, for tools and tests.
func ChangesOf(reg *extensions.Registry, deps func() Deps) func(args features.ServiceArgs, b live.Branch, head string) ([]PartRef, error) {
	r := newReader(reg, deps)
	return func(args features.ServiceArgs, b live.Branch, head string) ([]PartRef, error) {
		return r.Changes(args.Context(), args.Principal.TenantID, b, head)
	}
}
