package cognitionservice

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Traces is an engine.Traces keeping traces through svc (a proxy in a
// process without the tenant database, forge 21d-2), as the cognition
// service's principal in the run's tenant.
func Traces(svc Service) RemoteTraces { return RemoteTraces{svc} }

type RemoteTraces struct{ svc Service }

func args(ctx context.Context, tenant uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(tenant, "cognition")}
}

func failed(c features.ServiceResponse, what string) error {
	if c.Code == 200 {
		return nil
	}
	return logging.Code(c.ErrorCode).Errorf("cannot %s: %s", what, c.Message)
}

func (t RemoteTraces) Begin(ctx context.Context, rec cognition.Run) error {
	return failed(t.svc.SaveRun(SaveRunArgs{ServiceArgs: args(ctx, rec.TenantID), Begin: true, Run: rec}).Common, "keep the run")
}

func (t RemoteTraces) Save(ctx context.Context, rec cognition.Run, steps []cognition.Step) error {
	return failed(t.svc.SaveRun(SaveRunArgs{ServiceArgs: args(ctx, rec.TenantID), Run: rec, Steps: steps}).Common, "keep the trace")
}

func (t RemoteTraces) Get(ctx context.Context, tenant, runID uint64) (cognition.Run, []cognition.Step, bool, error) {
	resp := t.svc.ReadRun(ReadRunArgs{ServiceArgs: args(ctx, tenant), RunID: runID})
	return resp.Run, resp.Steps, resp.Found, failed(resp.Common, "read the trace")
}

func (t RemoteTraces) Purge(ctx context.Context, keepRuns, keepDays int) error {
	return failed(t.svc.PurgeRuns(PurgeRunsArgs{ServiceArgs: args(ctx, features.LocalTenantID), KeepRuns: keepRuns, KeepDays: keepDays}).Common, "purge traces")
}
