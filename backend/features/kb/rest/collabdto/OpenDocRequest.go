package collabdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// OpenDocRequest opens the room of a document of a kind: for "record",
// ref is {entity, id, field}.
type OpenDocRequest struct {
	Kind string          `json:"kind"`
	Ref  json.RawMessage `json:"ref"`
}

// ToArgs converts the request to the service's OpenDocArgs.
func (r OpenDocRequest) ToArgs(args features.ServiceArgs) collabservice.OpenDocArgs {
	return collabservice.OpenDocArgs{ServiceArgs: args, Kind: r.Kind, Ref: r.Ref}
}
