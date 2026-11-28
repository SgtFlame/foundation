package chronicle

import (
	"context"
	"slices"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
)

// ProjectAccess gives the project scopes ("project:<page>") whose entries
// a principal may read beside their own (forge 35f-6): Docs' writing
// projects, whose page subtree's readers share its story bible (35a-3
// sets it). Without one, a project's entries are its owner's alone.
type ProjectAccess func(ctx context.Context, p features.Principal) []string

var projectAccess atomic.Pointer[ProjectAccess]

// SetProjectAccess sets who reads a project's entries; nil leaves them to
// their owners.
func SetProjectAccess(fn ProjectAccess) {
	if fn == nil {
		projectAccess.Store(nil)
		return
	}
	projectAccess.Store(&fn)
}

// ReadableProjects are the project scopes p may read the entries of.
// Only people read projects: an agent or app gets none.
func ReadableProjects(ctx context.Context, p features.Principal) []string {
	fn := projectAccess.Load()
	if fn == nil || p.Kind != features.PrincipalUser {
		return nil
	}
	out := slices.DeleteFunc((*fn)(ctx, p), func(s string) bool { return !ValidScope(s) || s == ScopeTenant })
	slices.Sort(out)
	return slices.Compact(out)
}
