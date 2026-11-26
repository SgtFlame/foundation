package recorddto

import (
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

// RowProblems are why one row was not imported.
type RowProblems struct {
	Row      int            `json:"row"`
	Problems []live.Problem `json:"problems"`
}

type ImportRecordsResponse struct {
	rest.Status
	Created int           `json:"created"`
	IDs     []string      `json:"ids"`
	Failed  []RowProblems `json:"failed"`
}

// ImportRecordsResponseFrom converts the service's ImportResponse.
func ImportRecordsResponseFrom(resp recordservice.ImportResponse) ImportRecordsResponse {
	out := ImportRecordsResponse{Status: rest.StatusFrom(resp.Common), Created: resp.Created, IDs: resp.IDs, Failed: []RowProblems{}}
	if out.IDs == nil {
		out.IDs = []string{}
	}
	for _, f := range resp.Failed {
		out.Failed = append(out.Failed, RowProblems{Row: f.Row, Problems: f.Problems})
	}
	return out
}
