package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

type CreateRequest struct {
	EntryInput
}

// ToArgs converts the request to the service's CreateArgs.
func (r CreateRequest) ToArgs(args features.ServiceArgs) chronicleservice.CreateArgs {
	return chronicleservice.CreateArgs{ServiceArgs: args, EntryInput: r.EntryInput.toInput()}
}

func (in EntryInput) toInput() chronicleservice.EntryInput {
	return chronicleservice.EntryInput{
		Type: in.Type, Title: in.Title, ShortSummary: in.ShortSummary, FullSummary: in.FullSummary, FullContext: in.FullContext,
		Fields: in.Fields, ParentID: in.ParentID,
	}
}
