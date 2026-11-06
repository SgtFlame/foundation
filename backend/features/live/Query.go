package live

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Query is one named read. Its Descriptor's ID is the query, e.g.
// "hello.counter". Every query is a subscription: Run sends a snapshot,
// then changes, until the feed's context ends.
type Query struct {
	extensions.Descriptor
	Title       string
	Description string          // for a model: what it returns
	Params      json.RawMessage // the parameters' JSON Schema, an object; empty takes none
	Examples    []json.RawMessage
	// Action is the authz action it needs, e.g. "hello.read", checked on
	// Resource (the principal's tenant when nil) when the subscription
	// opens and again when grants change.
	Action   string
	Resource func(p features.Principal, params json.RawMessage) string
	// Run feeds the subscription until f.Context() ends; it returns early
	// only on failure.
	Run func(f Feed) error
}

// Feed is one open subscription, as its query sees it.
type Feed interface {
	Args() features.ServiceArgs // the subscriber's, with the subscription's context
	Context() context.Context
	Params() json.RawMessage
	Store() Store
	// Snapshot sends the whole result at pos; it may be sent again
	// (after a gap, or when a query re-runs).
	Snapshot(data any, pos Position)
	// Change sends one change at pos.
	Change(delta Delta, pos Position)
}

// Delta is one change to a query's result. Op is "replace" (Value is the
// whole result), "upsert" (Value at Key) or "remove" (Key), or "reached"
// (nothing changed, but the subscription has reached the change's
// position, so a client waiting for its own write knows it is applied).
//
// With List, the change is to the ordered list at that key of the result,
// an object, and Key names the item: "insert" (Value at Index), "update"
// (Value at From), "move" (the item at From taken out, and Value put at
// Index) or "remove" (the item at From). The indexes are the list's as the
// changes before it left it.
type Delta struct {
	Op    string          `json:"op"`
	Key   string          `json:"key,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
	List  string          `json:"list,omitempty"`
	Index *int            `json:"index,omitempty"`
	From  *int            `json:"from,omitempty"`
}

// Reached is the change that changes nothing (Delta's "reached").
var Reached = Delta{Op: "reached"}

// ValueOf is a query's Run over one projected key: the value, or Empty
// while there is none, then its replacements.
func ValueOf(projector string, key func(f Feed) (string, error), empty json.RawMessage) func(Feed) error {
	return func(f Feed) error {
		k, err := key(f)
		if err != nil {
			return err
		}
		entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, projector, k)
		if err != nil {
			return err
		}
		initial := true
		pos := Position{}
		var value json.RawMessage = empty
		for e := range entries {
			if e == nil {
				initial = false
				f.Snapshot(value, pos)
				continue
			}
			value = e.Value.Value
			if e.Deleted {
				value = empty
			}
			pos = pos.Merge(e.Value.Position())
			if !initial {
				f.Change(Delta{Op: "replace", Value: value}, pos)
			}
		}
		return nil
	}
}

// ListOf is a query's Run over the keys matching a pattern: a map from key
// to value, then upserts and removals.
func ListOf(projector string, pattern func(f Feed) (string, error)) func(Feed) error {
	return func(f Feed) error {
		p, err := pattern(f)
		if err != nil {
			return err
		}
		entries, err := f.Store().Watch(f.Context(), f.Args().Principal.TenantID, projector, p)
		if err != nil {
			return err
		}
		initial := map[string]json.RawMessage{}
		pos := Position{}
		started := false
		for e := range entries {
			if e == nil {
				started = true
				f.Snapshot(initial, pos)
				continue
			}
			pos = pos.Merge(e.Value.Position())
			switch {
			case !started && !e.Deleted:
				initial[e.Key] = e.Value.Value
			case !started:
				delete(initial, e.Key)
			case e.Deleted:
				f.Change(Delta{Op: "remove", Key: e.Key}, pos)
			default:
				f.Change(Delta{Op: "upsert", Key: e.Key, Value: e.Value.Value}, pos)
			}
		}
		return nil
	}
}

// Apply applies one change to a query's result held as decoded JSON (maps,
// slices and values), as a subscriber that keeps the result does, and
// returns the new result; data itself is left as it was.
func Apply(data any, d Delta) any {
	var value any
	if d.Value != nil {
		_ = json.Unmarshal(d.Value, &value)
	}
	if d.List != "" {
		m, _ := data.(map[string]any)
		if m == nil {
			return data
		}
		// Copied, never changed in place: a caller may hold the old data
		// (a UI session's rendered props share it)
		m = maps.Clone(m)
		list, _ := m[d.List].([]any)
		list = slices.Clone(list)
		at := func(p *int) int {
			if p == nil {
				return -1
			}
			return *p
		}
		from, to := at(d.From), at(d.Index)
		switch d.Op {
		case "insert":
			if to >= 0 && to <= len(list) {
				list = append(list[:to], append([]any{value}, list[to:]...)...)
			}
		case "update":
			if from >= 0 && from < len(list) {
				list[from] = value
			}
		case "move":
			if from >= 0 && from < len(list) {
				list = append(list[:from], list[from+1:]...)
				if to >= 0 && to <= len(list) {
					list = append(list[:to], append([]any{value}, list[to:]...)...)
				}
			}
		case "remove":
			if from >= 0 && from < len(list) {
				list = append(list[:from], list[from+1:]...)
			}
		}
		m[d.List] = list
		return m
	}
	switch d.Op {
	case "replace":
		return value
	case "upsert", "insert", "update":
		m, ok := data.(map[string]any)
		if !ok {
			m = map[string]any{}
		}
		m = maps.Clone(m)
		m[d.Key] = value
		return m
	case "remove":
		if m, ok := data.(map[string]any); ok {
			m = maps.Clone(m)
			delete(m, d.Key)
			return m
		}
	}
	return data
}
