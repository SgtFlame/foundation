// Package extensions lets features contribute to named extension points that
// other features declare, instead of being wired in by hand (see
// documentation/Contributors.md). There are no init() side effects:
// serve.go creates the Registry and calls each feature's Contribute(reg) in
// order, and tests build their own.
package extensions

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/logging"
)

// DefaultOrder sorts contributions that set no Order.
const DefaultOrder = 1000

// Descriptor identifies a contribution. Contributions embed it, which gives
// them the Describe method of Contribution.
type Descriptor struct {
	ID     string // unique within its point
	Order  int    // sort key; 0 means DefaultOrder
	Source string // the contributing feature, e.g. "chat"
}

func (d Descriptor) Describe() Descriptor {
	if d.Order == 0 {
		d.Order = DefaultOrder
	}
	return d
}

// Contribution is what a point accepts.
type Contribution interface {
	Describe() Descriptor
}

// AnyPoint is a Point of any contribution type.
type AnyPoint interface {
	ID() string
	Description() string
}

// Point is a typed token for an extension point, declared by the feature
// that owns it: var Sections = extensions.NewPoint[Section]("config.section", "...").
type Point[T Contribution] struct {
	id, description string
}

func NewPoint[T Contribution](id, description string) Point[T] {
	return Point[T]{id: id, description: description}
}

func (p Point[T]) ID() string          { return p.id }
func (p Point[T]) Description() string { return p.description }

// Register contributes c to p. A contribution with the same ID replaces the
// earlier one, with a warning.
func (p Point[T]) Register(reg *Registry, c T) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e := reg.entry(p)
	id := c.Describe().ID
	if i := slices.IndexFunc(e.contributions, func(old Contribution) bool { return old.Describe().ID == id }); i >= 0 {
		logging.In("extensions").With("point", p.id, "id", id).Warn(context.Background(), "%s: %s replaces an earlier contribution", p.id, id)
		e.contributions[i] = c
		return
	}
	e.contributions = append(e.contributions, c)
}

// All is p's contributions, sorted by Order and then ID.
func (p Point[T]) All(reg *Registry) []T {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	e := reg.points[p.id]
	if e == nil {
		return nil
	}
	out := make([]T, len(e.contributions))
	for i, c := range sorted(e.contributions) {
		out[i] = c.(T)
	}
	return out
}

// Registry holds the declared points and their contributions.
type Registry struct {
	mu     sync.RWMutex
	points map[string]*entry
	order  []string // declaration order
}

type entry struct {
	description   string
	contributions []Contribution
}

func NewRegistry() *Registry {
	return &Registry{points: map[string]*entry{}}
}

// Declare lists points even before anything contributes to them.
func (r *Registry) Declare(points ...AnyPoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range points {
		r.entry(p)
	}
}

func (r *Registry) entry(p AnyPoint) *entry {
	e := r.points[p.ID()]
	if e == nil {
		e = &entry{description: p.Description()}
		r.points[p.ID()] = e
		r.order = append(r.order, p.ID())
	}
	return e
}

// PointInfo describes a point and its contributions, for listing.
type PointInfo struct {
	ID            string
	Description   string
	Contributions []Descriptor
}

// Points describes every point, in the order they were declared.
func (r *Registry) Points() []PointInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PointInfo, 0, len(r.order))
	for _, id := range r.order {
		e := r.points[id]
		info := PointInfo{ID: id, Description: e.description, Contributions: []Descriptor{}}
		for _, c := range sorted(e.contributions) {
			info.Contributions = append(info.Contributions, c.Describe())
		}
		out = append(out, info)
	}
	return out
}

func sorted(cs []Contribution) []Contribution {
	out := slices.Clone(cs)
	slices.SortStableFunc(out, func(a, b Contribution) int {
		da, db := a.Describe(), b.Describe()
		return cmp.Or(cmp.Compare(da.Order, db.Order), cmp.Compare(da.ID, db.ID))
	})
	return out
}
