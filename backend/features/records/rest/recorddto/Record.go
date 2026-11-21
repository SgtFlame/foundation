package recorddto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// Record is a record as the records.get query gives it: values by field
// id, and its own data under keys starting with '_'.
type Record = map[string]json.RawMessage

// RecordsFrom converts the service's records.
func RecordsFrom(in []data.Record) []Record {
	out := make([]Record, 0, len(in))
	for _, r := range in {
		out = append(out, Record(r))
	}
	return out
}
