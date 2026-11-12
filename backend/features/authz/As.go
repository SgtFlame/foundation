package authz

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features"
)

// As is a role checks are decided by in place of a user's grants, for the
// actions and resources it covers: a draft app's role previewed by its
// author (forge 06e question 3), which no grant gives yet. What it does
// not cover is decided by the user's own grants.
type As struct {
	Name string // for the decision's reason
	// Covers reports whether the role decides action on resource, and
	// whether it allows it.
	Covers func(action, resource string) (covered, allowed bool)
	// Rules are the role's row rules by key (an entity type's id), for
	// the actions it covers.
	Rules map[string]string
}

type asKey struct{}

// WithAs is ctx whose user checks are decided by as where it covers them.
func WithAs(ctx context.Context, as As) context.Context {
	return context.WithValue(ctx, asKey{}, &as)
}

// asFor is the role ctx decides p's checks of action on resource by, when
// it covers them.
func asFor(ctx context.Context, p features.Principal, action, resource string) (as *As, allowed bool) {
	as, _ = ctx.Value(asKey{}).(*As)
	if as == nil || as.Covers == nil || p.Kind != features.PrincipalUser {
		return nil, false
	}
	covered, allowed := as.Covers(action, resource)
	if !covered {
		return nil, false
	}
	return as, allowed
}
