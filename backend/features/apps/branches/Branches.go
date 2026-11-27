// Package branches keeps an app's branches and commits (forge 19a and
// 19c, documentation/Apps.md › Built in forge 19a): a branch is a named
// overlay of drafts of the parts the app owns, made from a commit; its
// first change to a part copies the part's state at that commit (the
// live core's branched event), and a part it never changed reads as the
// commit has it. A commit freezes the parts a branch changed since its
// parent, each at its sequence, so a part's state at any commit is its
// events folded up to there. main is the app's drafts as they always
// were; its record and first commit are made the first time a branch is
// made from it or it is committed.
package branches

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/live"
)

var log = apps.Log.Tags("branches")

// The branch and commit aggregates on the def stream, and their
// projections.
const (
	TargetType      = "appBranch" // id: live.Branch.Key(), <app>_<name> (main too)
	Projector       = "apps.branch"
	CommitType      = "appCommit" // id: <app>-c<tsid>
	CommitProjector = "apps.commit"
)

// Statuses of a branch.
const (
	StatusOpen      = "open"
	StatusMerged    = "merged"
	StatusAbandoned = "abandoned"
)

// Origins of a branch: who made it.
const (
	OriginManual       = "manual"
	OriginBuilder      = "builder"
	OriginRequirements = "requirements"
	OriginMerge        = "merge" // a merge request's candidate (forge 19e)
)

// Events on a branch and on a commit.
const (
	EventCreated   = "created"   // branch: Record; commit: Commit
	EventCommitted = "committed" // branch: {commit}
	EventAbandoned = "abandoned" // branch: {}
	EventMerged    = "merged"    // branch: {into, commit}
	EventReopened  = "reopened"  // branch: {}
	// On a commit (forge 19d): its diffs, set once, and approvals.
	EventDiffed    = "diffed"    // {structured}
	EventExplained = "explained" // {semantic}
	EventApproved  = "approved"  // Approval
)

// Base is what a branch was made from: a branch and its commit then.
type Base struct {
	Branch string `json:"branch"`
	Commit string `json:"commit,omitempty"`
}

// Record is a branch, the apps.branch projection's value.
type Record struct {
	App    string `json:"app"`
	Name   string `json:"name"`
	Title  string `json:"title,omitempty"`
	Base   Base   `json:"base"`
	Status string `json:"status"`
	Origin string `json:"origin"`
	// Requirements is the requirements branch it answers (forge 19h), as
	// <space>:<branch>.
	Requirements string `json:"requirements,omitempty"`
	// Head is its latest commit, its base's commit before its first.
	Head       string    `json:"head,omitempty"`
	Commits    int       `json:"commits"`
	MergedInto string    `json:"mergedInto,omitempty"`
	CreatedBy  uint64    `json:"createdBy,string"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Branch is the record's branch.
func (r Record) Branch() live.Branch { return live.Branch{App: r.App, Name: r.Name} }

// PartRef is one part in a commit: its type and own id, the target its
// events are on (its draft on the commit's branch), and the sequence it
// is frozen at. Removed marks a part the commit removes.
type PartRef struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Target  string `json:"target"`
	Seq     uint64 `json:"seq"`
	Removed bool   `json:"removed,omitempty"`
}

// Key is the part's type and id, prefab.orderRow.
func (p PartRef) Key() string { return p.Type + "." + p.ID }

// Approval is one approval of a commit (forge 19d).
type Approval struct {
	By      uint64    `json:"by,string"`
	Kinds   []string  `json:"kinds"` // the part kinds the approver approves
	Comment string    `json:"comment,omitempty"`
	At      time.Time `json:"at"`
}

// Commit is an immutable record of the parts a branch changed since its
// parent, the apps.commit projection's value.
type Commit struct {
	ID          string    `json:"id"`
	App         string    `json:"app"`
	Branch      string    `json:"branch"`
	Parent      string    `json:"parent,omitempty"`
	MergeParent string    `json:"mergeParent,omitempty"`
	Parts       []PartRef `json:"parts"`
	Message     string    `json:"message"`
	Author      uint64    `json:"author,string"`
	CreatedAt   time.Time `json:"createdAt"`
	// Structured is each changed part's structured diff (forge 19d),
	// Semantic what it means; each is set once.
	Structured json.RawMessage `json:"structured,omitempty"`
	Semantic   json.RawMessage `json:"semantic,omitempty"`
	Approvals  []Approval      `json:"approvals,omitempty"`
	// Kinds are the approval kinds it touches, Approved whether each is
	// approved, Waiting those that are not yet (kept by the fold).
	Kinds    []string `json:"kinds,omitempty"`
	Approved bool     `json:"approved,omitempty"`
	Waiting  []string `json:"waiting,omitempty"`
}

// KeyOf is a branch record's target id.
func KeyOf(app, name string) string {
	if name == "" {
		name = live.Main
	}
	return app + "_" + name
}

// SplitKey reads a branch record's target id back.
func SplitKey(id string) (app, name string) {
	i := strings.LastIndex(id, "_")
	if i <= 0 {
		return id, ""
	}
	return id[:i], id[i+1:]
}

// AppOfCommit is a commit id's app.
func AppOfCommit(id string) string {
	i := strings.LastIndex(id, "-c")
	if i <= 0 {
		return ""
	}
	return id[:i]
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// GetRecord reads a branch's record; main has one only once it was
// committed or branched from.
func GetRecord(ctx context.Context, store live.Store, tenant uint64, app, name string) (Record, bool) {
	v, ok, err := store.Get(ctx, tenant, Projector, TargetType+"."+KeyOf(app, name))
	if err != nil || !ok || v.Value == nil {
		return Record{}, false
	}
	r, err := decode[Record](v.Value)
	return r, err == nil
}

// GetCommit reads a commit.
func GetCommit(ctx context.Context, store live.Store, tenant uint64, id string) (Commit, bool) {
	if !live.ValidToken(id) {
		return Commit{}, false
	}
	v, ok, err := store.Get(ctx, tenant, CommitProjector, CommitType+"."+id)
	if err != nil || !ok || v.Value == nil {
		return Commit{}, false
	}
	c, err := decode[Commit](v.Value)
	return c, err == nil
}

func foldBranch(f live.Fold) (json.RawMessage, error) {
	var r Record
	if f.State != nil {
		if err := json.Unmarshal(f.State, &r); err != nil {
			return nil, err
		}
	}
	e := f.Event
	switch e.Type {
	case EventCreated:
		v, err := decode[Record](e.Data)
		if err != nil {
			return nil, err
		}
		r = v
		r.CreatedBy, r.CreatedAt = e.UserID, e.Time
	case EventCommitted:
		v, err := decode[struct{ Commit string }](e.Data)
		if err != nil {
			return nil, err
		}
		r.Head = v.Commit
		r.Commits++
	case EventAbandoned:
		r.Status = StatusAbandoned
	case EventReopened:
		r.Status = StatusOpen
	case EventMerged:
		v, err := decode[struct{ Into string }](e.Data)
		if err != nil {
			return nil, err
		}
		r.Status, r.MergedInto = StatusMerged, v.Into
	default:
		return f.State, nil
	}
	r.UpdatedAt = e.Time
	return json.Marshal(r)
}

func foldCommit(f live.Fold) (json.RawMessage, error) {
	var c Commit
	if f.State != nil {
		if err := json.Unmarshal(f.State, &c); err != nil {
			return nil, err
		}
	}
	e := f.Event
	switch e.Type {
	case EventCreated:
		v, err := decode[Commit](e.Data)
		if err != nil {
			return nil, err
		}
		c = v
		c.Author, c.CreatedAt = e.UserID, e.Time
		if c.Parts == nil {
			c.Parts = []PartRef{}
		}
	case EventDiffed:
		v, err := decode[struct{ Structured json.RawMessage }](e.Data)
		if err != nil {
			return nil, err
		}
		c.Structured = v.Structured
	case EventExplained:
		v, err := decode[struct{ Semantic json.RawMessage }](e.Data)
		if err != nil {
			return nil, err
		}
		c.Semantic = v.Semantic
	case EventApproved:
		v, err := decode[Approval](e.Data)
		if err != nil {
			return nil, err
		}
		v.By, v.At = e.UserID, e.Time
		c.Approvals = append(c.Approvals, v)
	default:
		return f.State, nil
	}
	c.Kinds = KindsOf(c)
	c.Approved, c.Waiting = ApprovalStatus(c)
	return json.Marshal(c)
}
