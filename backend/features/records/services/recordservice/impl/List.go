package impl

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// List reads an entity type's records, oldest first.
func (s *service) List(args recordservice.ListArgs) recordservice.ListResponse {
	raw, fail := s.once(args.ServiceArgs, "records.list", data.ListParams{Entity: args.Entity, Where: args.Where})
	if fail != nil {
		return recordservice.ListResponse{Common: *fail}
	}
	var byKey map[string]data.Record
	_ = json.Unmarshal(raw, &byKey)
	out := make([]data.Record, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	// Record ids are TSIDs, so a longer id is a later one
	slices.SortFunc(out, func(a, b data.Record) int {
		x, y := string(a[data.MetaID]), string(b[data.MetaID])
		if len(x) != len(y) {
			return len(x) - len(y)
		}
		return strings.Compare(x, y)
	})
	return recordservice.ListResponse{Common: features.OK(), Records: out}
}
