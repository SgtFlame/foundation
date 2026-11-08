package live

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Projector folds one area's events into a KV bucket per tenant. Its
// Descriptor's ID names the bucket (PRJ_<tenant>_<id>, dots as '_').
type Projector struct {
	extensions.Descriptor
	Title       string
	Description string // for a model: what the projection holds
	Area        Area
	Types       []string // the target types it folds; empty folds every type in the area
	// Fold is the key's next value after the event, from its current one
	// (nil when there is none). A nil result deletes the key.
	Fold func(f Fold) (json.RawMessage, error)
	// KeyOf, when set, is the key an event folds into in place of its
	// target's; "" skips the event. The operation log keys by op.
	KeyOf func(e Event) string
	// Keep, when set, keeps keys outside the projection in step with each
	// value, in the same fold: it runs after Fold and before the value is
	// written, given the keys it kept for the old value, and returns the
	// keys it keeps for the new one, which are stored with the value. An
	// error retries the event. Records keep their index entries this way.
	Keep func(k Keep) ([]string, error)
}

// Keep is one folded value whose kept keys are brought up to date.
type Keep struct {
	Ctx    context.Context
	Tenant uint64
	Key    string
	Value  json.RawMessage // the new value; nil when the key is deleted
	Kept   []string        // the keys kept for the old value
}

// Fold is one event to fold into one key.
type Fold struct {
	Key   string
	State json.RawMessage
	Event Event
}

// Bucket is the projector's bucket in a tenant.
func Bucket(tenant uint64, projector string) string {
	return fmt.Sprintf("PRJ_%d_%s", tenant, strings.ReplaceAll(projector, ".", "_"))
}

// Projected is a value in a projection, with the stream and sequence of the
// last event folded into it.
type Projected struct {
	Stream string          `json:"stream"`
	Seq    uint64          `json:"seq"`
	Value  json.RawMessage `json:"value"`
	Kept   []string        `json:"kept,omitempty"` // the keys the projector's Keep keeps for it
}

// Position is the projected value's position.
func (p Projected) Position() Position {
	if p.Stream == "" {
		return Position{}
	}
	return Position{p.Stream: p.Seq}
}

// Entry is one key's change in a watched projection. Deleted is set when the
// key went away; Initial when it was there as the watch began.
type Entry struct {
	Key     string
	Value   Projected
	Deleted bool
	Initial bool
}

// Store reads projections. live/project implements it.
type Store interface {
	// Get is a key's value; ok is false when it has none.
	Get(ctx context.Context, tenant uint64, projector, key string) (v Projected, ok bool, err error)
	// Watch sends the keys matching pattern (a KV key filter such as
	// "counter.*") as they are, then nil once, then each change, until ctx
	// ends.
	Watch(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *Entry, error)
}
