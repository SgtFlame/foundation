package connectorsservice

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

// Remote is connectors.Books through the service (forge 21e-1): an adapter
// given it as adapter.Options.Books holds no JetStream handle.
func Remote(svc Service) connectors.Books { return remote{svc} }

type remote struct{ svc Service }

// as is a call as the connectors' service principal in tenant.
func as(ctx context.Context, tenant uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipalFor(tenant, "connectors")}
}

type failure struct{ r features.ServiceResponse }

func (f failure) Error() string { return f.r.Message }

func failed(r features.ServiceResponse) error {
	switch {
	case r.Code >= 200 && r.Code < 300:
		return nil
	case r.ErrorCode == "lease_taken":
		return connectors.ErrLeaseTaken
	case r.ErrorCode == "lease_lost":
		return connectors.ErrLeaseLost
	case r.ErrorCode == "bad_event":
		return connectors.Errorf(connectors.CodeInvalid, "%s", r.Message)
	case r.Code == 503 || r.Code == 504:
		return connectors.Errorf(connectors.CodeUnavailable, "%s", r.Message)
	}
	return failure{r}
}

func (x remote) Announce(ctx context.Context, a connectors.Announcement) error {
	return failed(x.svc.Announce(AnnounceArgs{ServiceArgs: as(ctx, 0), Announcement: a}).Common)
}

func (x remote) Withdraw(ctx context.Context, key string) error {
	return failed(x.svc.Withdraw(KeyArgs{ServiceArgs: as(ctx, 0), Key: key}).Common)
}

func (x remote) Announcements(ctx context.Context) ([]connectors.Announcement, error) {
	r := x.svc.Announcements(AnnouncementsArgs{ServiceArgs: as(ctx, 0)})
	return r.Announcements, failed(r.Common)
}

func (x remote) PutDiscovered(ctx context.Context, tenant uint64, connection string, c connectors.Contract) (connectors.Discovered, error) {
	r := x.svc.PutDiscovered(PutDiscoveredArgs{ServiceArgs: as(ctx, tenant), Connection: connection, Contract: c})
	return r.Discovered, failed(r.Common)
}

func (x remote) LedgerPut(ctx context.Context, tenant uint64, connection, op, key string, e connectors.LedgerEntry) error {
	return failed(x.svc.LedgerPut(LedgerPutArgs{ServiceArgs: as(ctx, tenant), Connection: connection, Operation: op, Key: key, Entry: e}).Common)
}

// Lock tries until ctx ends, as connectors.Lock does; a lock whose holder
// died ends by the bucket's TTL.
func (x remote) Lock(ctx context.Context, tenant uint64, connection, holder string) (func(), error) {
	for {
		r := x.svc.TryLock(TryLockArgs{ServiceArgs: as(ctx, tenant), Connection: connection, Holder: holder})
		if err := failed(r.Common); err != nil {
			return nil, err
		}
		if r.Held {
			rev := r.Rev
			return func() {
				bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = x.svc.Unlock(UnlockArgs{ServiceArgs: as(bg, tenant), Connection: connection, Rev: rev})
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, connectors.Errorf(connectors.CodeUnavailable, "connection %s is in use; try again", connection)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (x remote) Sources(ctx context.Context) (<-chan connectors.SourceUpdate, error) {
	r := x.svc.Sources(SourcesArgs{ServiceArgs: as(ctx, 0)})
	if r == nil || r.Updates == nil {
		return nil, errors.New("the event sources cannot be watched")
	}
	return r.Updates, nil
}

func (x remote) LeaseCreate(ctx context.Context, key string, l connectors.Lease) (uint64, error) {
	r := x.svc.LeaseCreate(LeaseArgs{ServiceArgs: as(ctx, 0), Key: key, Lease: l})
	return r.Rev, failed(r.Common)
}

func (x remote) LeaseUpdate(ctx context.Context, key string, l connectors.Lease, rev uint64) (uint64, error) {
	r := x.svc.LeaseUpdate(LeaseArgs{ServiceArgs: as(ctx, 0), Key: key, Lease: l, Rev: rev})
	return r.Rev, failed(r.Common)
}

func (x remote) LeaseDelete(ctx context.Context, key string, rev uint64) error {
	return failed(x.svc.LeaseDelete(LeaseArgs{ServiceArgs: as(ctx, 0), Key: key, Rev: rev}).Common)
}

func (x remote) Cursor(ctx context.Context, key string) (connectors.Cursor, bool, error) {
	r := x.svc.Cursor(KeyArgs{ServiceArgs: as(ctx, 0), Key: key})
	return r.Cursor, r.Found, failed(r.Common)
}

func (x remote) PutCursor(ctx context.Context, key string, c connectors.Cursor) error {
	return failed(x.svc.PutCursor(PutCursorArgs{ServiceArgs: as(ctx, 0), Key: key, Cursor: c}).Common)
}

func (x remote) Publish(ctx context.Context, tenant uint64, connection, contract, event, id string, payload json.RawMessage, at time.Time) error {
	return failed(x.svc.Publish(PublishArgs{ServiceArgs: as(ctx, tenant), Connection: connection, Contract: contract, Event: event, ID: id, Payload: payload, At: at}).Common)
}
