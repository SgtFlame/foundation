package live

import (
	"context"
	"encoding/json"

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
// whole result), "upsert" (Value at Key) or "remove" (Key).
type Delta struct {
	Op    string          `json:"op"`
	Key   string          `json:"key,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

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
