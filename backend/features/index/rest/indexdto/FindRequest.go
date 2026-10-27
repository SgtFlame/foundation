package indexdto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
)

type FindRequest struct {
	Query     string  `json:"query"`
	Weight    float64 `json:"weight"` // 0..1, the vector search's share
	Count     int     `json:"count,omitempty"`
	VolumeIDs TSIDs   `json:"volumeIds,omitempty"`
}

// ToArgs converts the request to the service's FindArgs.
func (r FindRequest) ToArgs(args features.ServiceArgs) indexservice.FindArgs {
	return indexservice.FindArgs{
		ServiceArgs: args,
		Query:       r.Query,
		Weight:      r.Weight,
		Count:       r.Count,
		VolumeIDs:   r.VolumeIDs,
	}
}
