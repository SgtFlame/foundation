package usageservice

import (
	"context"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/usage"
)

// Store is a usage.Store through svc (a proxy in a process without the
// tenant database, forge 21d-2), as the usage service's principal.
func Store(svc Service) usage.Store { return store{svc} }

type store struct{ svc Service }

func serviceArgs(ctx context.Context) features.ServiceArgs {
	return features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(features.LocalTenantID, "usage")}
}

func fail(c features.ServiceResponse) error {
	if c.Code == 200 {
		return nil
	}
	return logging.Code(c.ErrorCode).Errorf("%s", c.Message)
}

func (s store) Append(ctx context.Context, records []usage.Record, captures []usage.Capture) error {
	return fail(s.svc.Append(AppendArgs{ServiceArgs: serviceArgs(ctx), Records: records, Captures: captures}).Common)
}

func (s store) PurgeCaptures(ctx context.Context, before time.Time) (int64, error) {
	r := s.svc.PurgeCaptures(PurgeCapturesArgs{ServiceArgs: serviceArgs(ctx), Before: before})
	return r.Deleted, fail(r.Common)
}
