package impl

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// Get reads one record.
func (s *service) Get(args recordservice.GetArgs) recordservice.GetResponse {
	raw, fail := s.once(args.ServiceArgs, "records.get", map[string]string{"entity": args.Entity, "id": args.ID})
	if fail != nil {
		return recordservice.GetResponse{Common: *fail}
	}
	var r data.Record
	_ = json.Unmarshal(raw, &r)
	return recordservice.GetResponse{Common: features.OK(), Record: r}
}
