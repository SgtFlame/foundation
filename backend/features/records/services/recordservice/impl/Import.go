package impl

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
)

// maxFailed is how many failed rows an import reports.
const maxFailed = 20

// Import creates a record per row, a record.create each, several at once
// (the records section's importConcurrency). Each command's id names the
// import and the row, so resending an import within the bus's duplicate
// window creates nothing twice.
func (s *service) Import(args recordservice.ImportArgs) recordservice.ImportResponse {
	resource := records.Resource(args.Principal, args.Entity)
	if err := authz.Check(args.Context(), args.Principal, "record.create", resource).Err("record.create", resource); err != nil {
		return recordservice.ImportResponse{Common: *args.Fail(authz.Status, err)}
	}
	if len(args.Rows) == 0 {
		return recordservice.ImportResponse{Common: features.OK(), IDs: []string{}}
	}
	settings := recordsSettings(s, args.ServiceArgs)
	batch := features.NewTSID()
	ids := make([]string, len(args.Rows))
	failed := make([]*recordservice.RowProblems, len(args.Rows))
	var fatal *features.ServiceResponse
	var mu sync.Mutex
	work := make(chan int)
	var wg sync.WaitGroup
	for range max(1, settings.ImportConcurrency) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				res := s.command(args.ServiceArgs, fmt.Sprintf("import-%d-%d", batch, i), "record.create", "", map[string]any{"entity": args.Entity, "values": args.Rows[i]})
				switch {
				case res.Common.Code < 300:
					ids[i] = res.ID
				case res.Common.Code == http.StatusUnprocessableEntity:
					failed[i] = &recordservice.RowProblems{Row: i, Problems: res.Problems}
				default:
					mu.Lock()
					if fatal == nil {
						c := res.Common
						fatal = &c
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := range args.Rows {
		mu.Lock()
		stop := fatal != nil
		mu.Unlock()
		if stop {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()
	resp := recordservice.ImportResponse{Common: features.OK(), IDs: ids}
	if fatal != nil {
		resp.Common = *fatal
	}
	for i := range args.Rows {
		if ids[i] != "" {
			resp.Created++
		} else if failed[i] != nil && len(resp.Failed) < maxFailed {
			resp.Failed = append(resp.Failed, *failed[i])
		}
	}
	return resp
}
