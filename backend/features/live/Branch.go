package live

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Branches of definitions (forge 19a, documentation/Apps.md › Built in
// forge 19a): an app's branch is a named overlay of drafts of the parts
// the app owns. A part's draft on a branch is its own target, with the id
// BranchTargetID(part, branch), in the same projection as main's, so every
// family's fold, check and inverse work on it unchanged. A part with no
// draft on a branch reads as its base has it (Branching.Base); its first
// change there copies the base's state with a branched event, in the same
// batch as the change. main is the plain ids, as before branches.

// Main is the branch every app has: its drafts as they always were.
const Main = "main"

// BranchMark separates a part's id from its branch in a branch draft's
// target id: prefab orderRow on branch refunds of app shop is
// orderRow__b_shop_refunds. Ids and branch names never hold it.
const BranchMark = "__b_"

// EventBranched is the event that copies a part's state from its base
// into its draft on a branch: {from, state}. A branchable projector takes
// its state as the value; other projectors skip it.
const EventBranched = "branched"

// Branched is the branched event's data.
type Branched struct {
	From  uint64          `json:"from"`  // the base state's sequence
	State json.RawMessage `json:"state"` // the base's projected value
}

// Removed is the value a branchable projection keeps for a part its
// branch removed (a graph's draft is deleted): the base does not show
// through, and reads find nothing.
var Removed = json.RawMessage(`{"$removed":true}`)

// IsRemoved reports whether a projected value is a branch's tombstone.
func IsRemoved(v json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(v), Removed) }

// MainOnly reports whether a definition command is sent on main only:
// publishing, and making, deleting and restoring a whole app.
func MainOnly(command string) bool {
	switch command {
	case "app.create", "app.delete", "app.restore", "app.import":
		return true
	}
	return strings.HasSuffix(command, ".publish")
}

// Branch names one app's branch. The zero value, and Name main, is the
// app's main line.
type Branch struct {
	App  string `json:"app"`
	Name string `json:"name"`
}

// IsMain reports whether b is the main line.
func (b Branch) IsMain() bool { return b.Name == "" || b.Name == Main }

// String is app:name, or app for main.
func (b Branch) String() string {
	if b.IsMain() {
		return b.App
	}
	return b.App + ":" + b.Name
}

// ParseBranch reads "<app>:<name>" (or "<app>" for main); ok is false
// for anything else.
func ParseBranch(s string) (Branch, bool) {
	app, name, _ := strings.Cut(s, ":")
	if app == "" || !ValidToken(app) || (name != "" && name != Main && !ValidBranchName(name)) {
		return Branch{}, false
	}
	return Branch{App: app, Name: name}, true
}

// Key is the branch's part of a branch draft's id: <app>_<name>.
func (b Branch) Key() string { return b.App + "_" + b.Name }

var branchName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// ValidBranchName reports whether name can name a branch: lower-case
// letters, digits and '-', at most 48, never main (git's rules, narrowed
// to what a subject token holds).
func ValidBranchName(name string) bool {
	return branchName.MatchString(name) && name != Main && !strings.HasPrefix(name, "-")
}

// BranchTargetID is a part's target id on a branch: the plain id on main.
func BranchTargetID(id string, b Branch) string {
	if b.IsMain() {
		return id
	}
	return id + BranchMark + b.Key()
}

// SplitBranchID reads a target id back: the part's own id and the branch
// it is a draft on (main for a plain id).
func SplitBranchID(id string) (part string, b Branch) {
	i := strings.LastIndex(id, BranchMark)
	if i < 0 {
		return id, Branch{}
	}
	key := id[i+len(BranchMark):]
	j := strings.LastIndex(key, "_")
	if j <= 0 {
		return id, Branch{}
	}
	return id[:i], Branch{App: key[:j], Name: key[j+1:]}
}

// PartID is a target id's part, without its branch.
func PartID(id string) string {
	p, _ := SplitBranchID(id)
	return p
}

// IsBranchID reports whether a target id is a draft on a branch.
func IsBranchID(id string) bool {
	_, b := SplitBranchID(id)
	return !b.IsMain()
}

// OnBranch is a target as it is drafted on a branch.
func (t Target) OnBranch(b Branch) Target {
	t.ID = BranchTargetID(t.ID, b)
	return t
}

// Branching is the live.branching point's contribution (forge 19a): what
// a branch reads where it has no draft of its own, and what it may
// change. The apps feature contributes the one implementation.
type Branching struct {
	extensions.Descriptor
	// Base is target t's state in projector as branch b's base has it:
	// the branch it was made from, at the commit it was made from (forge
	// 19c), down to main. ok is false when the base has none.
	Base func(ctx context.Context, tenant uint64, b Branch, projector string, t Target) (v Projected, ok bool, err error)
	// Check refuses a change on branch b to target t, whose state as the
	// branch has it is state (nil for a part the branch makes): a part
	// the app does not own, or a branch that is not open.
	Check func(ctx context.Context, tenant uint64, b Branch, projector string, t Target, state json.RawMessage) *Problem
}

// Branchings is the live.branching point.
var Branchings = extensions.NewPoint[Branching]("live.branching", "Branches of definitions: what a branch reads where it has no draft, and what it may change (forge 19a)")

// BranchingOf is the registry's branching, if any contributes one.
func BranchingOf(reg *extensions.Registry) (Branching, bool) {
	all := Branchings.All(reg)
	if len(all) == 0 {
		return Branching{}, false
	}
	return all[len(all)-1], true
}

// BranchableTypes are the target types a registry's branchable
// projectors fold.
func BranchableTypes(reg *extensions.Registry) map[string]string {
	out := map[string]string{}
	for _, pr := range Projectors.All(reg) {
		if pr.Branchable {
			for _, t := range pr.Types {
				out[t] = pr.ID
			}
		}
	}
	return out
}

// Apply folds one event as the projector does, with branches: a branched
// event is the value for a branchable projector and skipped (skip set)
// by every other, a tombstone reads as no value, and a branchable
// projector's nil for a branch draft is kept as a tombstone.
func (pr Projector) Apply(f Fold) (value json.RawMessage, skip bool, err error) {
	if IsRemoved(f.State) {
		f.State = nil
	}
	if f.Event.Type == EventBranched {
		if !pr.Branchable {
			return f.State, true, nil
		}
		var b Branched
		if err := json.Unmarshal(f.Event.Data, &b); err != nil {
			return nil, false, err
		}
		if len(b.State) == 0 || string(b.State) == "null" {
			return nil, false, nil // a draft discarded: the base shows through
		}
		return b.State, false, nil
	}
	v, err := pr.Fold(f)
	if err == nil && v == nil && pr.Branchable && IsBranchID(f.Event.Target.ID) {
		v = Removed
	}
	return v, false, err
}
