package indexdto

import (
	"encoding/json"
	"strconv"
)

// TSIDs is a list of IDs that travels as a JSON array of strings, like every
// uint64 (the ",string" tag option does not apply to slices).
type TSIDs []uint64

func (ids TSIDs) MarshalJSON() ([]byte, error) {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatUint(id, 10)
	}
	return json.Marshal(out)
}

func (ids *TSIDs) UnmarshalJSON(data []byte) error {
	var in []string
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	out := make(TSIDs, len(in))
	for i, s := range in {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		out[i] = id
	}
	*ids = out
	return nil
}
