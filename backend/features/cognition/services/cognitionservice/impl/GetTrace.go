package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/services/cognitionservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"gorm.io/gorm"
)

// GetTrace returns one of the principal's runs with its steps, and what
// each node's model calls used.
func (s *service) GetTrace(args cognitionservice.GetTraceArgs) cognitionservice.GetTraceResponse {
	if failed := check(args.ServiceArgs, "cognition.read", "runs"); failed != nil {
		return cognitionservice.GetTraceResponse{Common: *failed}
	}
	// A new chain per query: a GORM chain keeps the table of its last query
	tenant := func() *gorm.DB { return s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)) }
	var run cognition.Run
	err := tenant().Where("owner_id = ?", args.Principal.UserID).First(&run, args.RunID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cognitionservice.GetTraceResponse{Common: *args.Fail(http.StatusNotFound, log.Code("run_not_found").With("runId", args.RunID).New("there is no such run; its trace may have expired"))}
	}
	if err != nil {
		return cognitionservice.GetTraceResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp := cognitionservice.GetTraceResponse{Common: features.OK(), Run: run}
	if err := tenant().Where("run_id = ?", run.ID).Order("seq").Find(&resp.Steps).Error; err != nil {
		return cognitionservice.GetTraceResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	err = tenant().Model(&usage.Record{}).Where("run_id = ?", run.ID).
		Select("node_id, COUNT(*) AS calls, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, SUM(thinking_tokens) AS thinking_tokens, SUM(cost_micros) AS cost_micros, MAX(currency) AS currency").
		Group("node_id").Order("node_id").Scan(&resp.Usage).Error
	if err != nil {
		return cognitionservice.GetTraceResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return resp
}
