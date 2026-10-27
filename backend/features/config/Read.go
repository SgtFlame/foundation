package config

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/events"
)

// Reader resolves a section as args' principal sees it, secrets in plain
// text. configservice implements it; it is for Go callers, never a client.
type Reader interface {
	Resolved(args features.ServiceArgs, section string) (json.RawMessage, error)
}

// Read decodes the resolved section into T, so services never handle raw JSON.
func Read[T any](r Reader, args features.ServiceArgs, section string) (T, error) {
	var out T
	raw, err := r.Resolved(args, section)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

// Changed is published when a section's stored value changes at a scope.
type Changed struct {
	Section string
	Scope   string // app, tenant:<id> or user:<id>
}

// Changes carries Changed events to the services that cache settings.
type Changes = events.Bus[Changed]

func NewChanges() *Changes {
	return events.NewBus[Changed]()
}
