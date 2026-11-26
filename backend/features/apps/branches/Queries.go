package branches

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// BranchView is one branch in app.branches.
type BranchView struct {
	Record
	// Changed are the parts it changed since its head: what a commit now
	// would hold.
	Changed []PartRef `json:"changed"`
}

// Branches is app.branches' result: main first, then the others, newest
// first.
type Branches struct {
	App      string       `json:"app"`
	Branches []BranchView `json:"branches"`
}

// History is app.history's result: commits newest first.
type History struct {
	App     string   `json:"app"`
	Commits []Commit `json:"commits"`
}

func appParam(raw json.RawMessage) (string, string, error) {
	var p struct{ App, Branch string }
	_ = json.Unmarshal(raw, &p)
	if !live.ValidToken(p.App) {
		return "", "", fmt.Errorf("%q is not an app id", p.App)
	}
	return p.App, p.Branch, nil
}

// watchAll follows several projections' keys and calls changed, debounced,
// whenever any moves after they all sent their snapshots.
func watchAll(f live.Feed, store Projections, watches [][2]string, changed func(initial bool, pos live.Position)) error {
	type ev struct {
		e *live.Entry
	}
	merged := make(chan ev)
	for _, w := range watches {
		entries, err := store.WatchDrafts(f.Context(), f.Args().Principal.TenantID, w[0], w[1])
		if err != nil {
			return err
		}
		go func() {
			for e := range entries {
				select {
				case merged <- ev{e}:
				case <-f.Context().Done():
					return
				}
			}
		}()
	}
	pending := len(watches)
	pos := live.Position{}
	var timer <-chan time.Time
	for {
		select {
		case <-f.Context().Done():
			return nil
		case <-timer:
			timer = nil
			changed(false, pos)
		case m := <-merged:
			if m.e == nil {
				if pending--; pending == 0 {
					changed(true, pos)
				}
				continue
			}
			pos = pos.Merge(m.e.Value.Position())
			if pending == 0 && timer == nil {
				timer = time.After(150 * time.Millisecond)
			}
		}
	}
}

// branchesOf reads an app's branches with their changes.
func (r *reader) branchesOf(ctx context.Context, tenant uint64, app string) (Branches, error) {
	out := Branches{App: app, Branches: []BranchView{}}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := r.deps().Projections.Watch(ctx, tenant, Projector, TargetType+".*")
	if err != nil {
		return out, err
	}
	var recs []Record
	for e := range entries {
		if e == nil {
			break
		}
		if rec, err := decode[Record](e.Value.Value); err == nil && rec.App == app {
			recs = append(recs, rec)
		}
	}
	if !slices.ContainsFunc(recs, func(r Record) bool { return r.Name == live.Main }) {
		recs = append(recs, Record{App: app, Name: live.Main, Status: StatusOpen, Origin: OriginManual})
	}
	slices.SortFunc(recs, func(a, b Record) int {
		switch {
		case a.Name == live.Main:
			return -1
		case b.Name == live.Main:
			return 1
		}
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	for _, rec := range recs {
		v := BranchView{Record: rec, Changed: []PartRef{}}
		if rec.Status == StatusOpen && (rec.Head != "" || rec.Name != live.Main) {
			if v.Changed, err = r.Changes(ctx, tenant, rec.Branch(), rec.Head); err != nil {
				return out, err
			}
		}
		out.Branches = append(out.Branches, v)
	}
	return out, nil
}

// historyOf is an app's commits, newest first; with branch, only those
// its head reaches.
func (r *reader) historyOf(ctx context.Context, tenant uint64, app, branch string) (History, error) {
	out := History{App: app, Commits: []Commit{}}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := r.deps().Projections.Watch(ctx, tenant, CommitProjector, CommitType+".*")
	if err != nil {
		return out, err
	}
	byID := map[string]Commit{}
	for e := range entries {
		if e == nil {
			break
		}
		if c, err := decode[Commit](e.Value.Value); err == nil && c.App == app {
			byID[c.ID] = c
		}
	}
	keep := func(Commit) bool { return true }
	if branch != "" {
		reach := map[string]bool{}
		if rec, ok := r.record(ctx, tenant, app, branch); ok {
			var walk func(id string)
			walk = func(id string) {
				for id != "" && !reach[id] {
					reach[id] = true
					c := byID[id]
					walk(c.MergeParent)
					id = c.Parent
				}
			}
			walk(rec.Head)
		}
		keep = func(c Commit) bool { return reach[c.ID] }
	}
	for _, c := range byID {
		if keep(c) {
			out.Commits = append(out.Commits, c)
		}
	}
	slices.SortFunc(out.Commits, func(a, b Commit) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

func contributeQueries(reg *extensions.Registry, r *reader) {
	resource := func(p features.Principal, params json.RawMessage) string {
		app, _, _ := appParam(params)
		return apps.Resource(p, app)
	}
	drafts := func() [][2]string {
		var out [][2]string
		for _, pr := range live.Projectors.All(reg) {
			if pr.Branchable {
				for _, t := range pr.Types {
					out = append(out, [2]string{pr.ID, t + ".*"})
				}
			}
		}
		return out
	}
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "app.branches", Source: "apps"}, Title: "An app's branches",
		Description: "An app's branches, main first: {app, branches: [{app, name, title, base: {branch, commit}, status (open, merged, abandoned), origin (manual, builder, requirements, merge), requirements, head, commits, mergedInto, createdBy, createdAt, changed: [{type, id, target, seq, removed}]}]}. changed are the parts a branch changed since its head, which app.commit would commit; main's head is empty until it is first committed or branched from.",
		Params:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"}},"required":["app"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"tasks"}`)},
		Action:      "app.read", Resource: resource,
		Run: func(f live.Feed) error {
			app, _, err := appParam(f.Params())
			if err != nil {
				return err
			}
			last := ""
			watches := append([][2]string{{Projector, TargetType + ".*"}, {CommitProjector, CommitType + ".*"}}, drafts()...)
			return watchAll(f, r.deps().Projections, watches, func(initial bool, pos live.Position) {
				v, err := r.branchesOf(f.Context(), f.Args().Principal.TenantID, app)
				if err != nil {
					log.Error(f.Context(), err, "cannot read the branches of %s", app)
					return
				}
				raw, _ := json.Marshal(v)
				switch {
				case initial:
					f.Snapshot(v, pos)
				case string(raw) != last:
					f.Change(live.Delta{Op: "replace", Value: raw}, pos)
				default:
					f.Change(live.Reached, pos)
				}
				last = string(raw)
			})
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "app.history", Source: "apps"}, Title: "An app's history",
		Description: "An app's commits, newest first: {app, commits: [{id, app, branch, parent, mergeParent, parts, message, author, createdAt, structured, semantic, approvals}]}. With branch, only the commits its head reaches (through merges too).",
		Params:      json.RawMessage(`{"type":"object","properties":{"app":{"type":"string","description":"The app's id"},"branch":{"type":"string","description":"Only the commits this branch's head reaches"}},"required":["app"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"app":"tasks"}`)},
		Action:      "app.read", Resource: resource,
		Run: func(f live.Feed) error {
			app, branch, err := appParam(f.Params())
			if err != nil {
				return err
			}
			last := ""
			return watchAll(f, r.deps().Projections, [][2]string{{Projector, TargetType + ".*"}, {CommitProjector, CommitType + ".*"}}, func(initial bool, pos live.Position) {
				v, err := r.historyOf(f.Context(), f.Args().Principal.TenantID, app, branch)
				if err != nil {
					log.Error(f.Context(), err, "cannot read the history of %s", app)
					return
				}
				raw, _ := json.Marshal(v)
				switch {
				case initial:
					f.Snapshot(v, pos)
				case string(raw) != last:
					f.Change(live.Delta{Op: "replace", Value: raw}, pos)
				default:
					f.Change(live.Reached, pos)
				}
				last = string(raw)
			})
		},
	})
}
