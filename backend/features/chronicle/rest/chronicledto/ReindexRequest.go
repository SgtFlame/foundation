package chronicledto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
)

type ReindexRequest struct {
	EntryIDs indexdto.TSIDs `json:"entryIds,omitempty"` // none reindexes whatever changed
}

// ToArgs converts the request to the service's ReindexArgs.
func (r ReindexRequest) ToArgs(args features.ServiceArgs) chronicleservice.ReindexArgs {
	return chronicleservice.ReindexArgs{ServiceArgs: args, EntryIDs: r.EntryIDs}
}
