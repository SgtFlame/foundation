// Package connectorsservice is the connectors' data service (forge
// 21e-1): the cx_* buckets and CX_<tenant> streams an adapter runtime keeps
// its books in, so an adapter needs no JetStream handle (Remote). Every
// operation is for Go only and for service principals only (403
// services_only); a per-tenant one works in the principal's tenant.
package connectorsservice

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
)

type Service interface {
	// Announce writes an adapter instance's announcement. Go only.
	//svc:idempotent
	Announce(args AnnounceArgs) Response
	// Withdraw removes one at once. Go only.
	//svc:idempotent
	Withdraw(args KeyArgs) Response
	// Announcements are the current ones. Go only.
	//svc:idempotent
	Announcements(args AnnouncementsArgs) AnnouncementsResponse
	// PutDiscovered stores a connection's discovered contract. Go only.
	//svc:idempotent
	PutDiscovered(args PutDiscoveredArgs) PutDiscoveredResponse
	// LedgerPut writes what an outside service accepted. Go only.
	//svc:idempotent
	LedgerPut(args LedgerPutArgs) Response
	// TryLock tries once to take a connection's exclusive lock. Go only.
	TryLock(args TryLockArgs) TryLockResponse
	// Unlock gives it back. Go only.
	//svc:idempotent
	Unlock(args UnlockArgs) Response
	// Sources sends the event sources, then their changes. Go only.
	//svc:stream
	Sources(args SourcesArgs) *SourcesRequest
	// LeaseCreate, LeaseUpdate and LeaseDelete keep event sources'
	// leases (409 lease_taken, lease_lost). Go only.
	LeaseCreate(args LeaseArgs) LeaseResponse
	LeaseUpdate(args LeaseArgs) LeaseResponse
	//svc:idempotent
	LeaseDelete(args LeaseArgs) Response
	// Cursor and PutCursor keep where a source resumes. Go only.
	//svc:idempotent
	Cursor(args KeyArgs) CursorResponse
	//svc:idempotent
	PutCursor(args PutCursorArgs) Response
	// Publish puts one connector event in CX_<tenant>, kept once by its
	// id. Go only.
	//svc:idempotent
	Publish(args PublishArgs) Response
}

// Response is an operation's outcome alone.
type Response struct {
	Common features.ServiceResponse
}

type AnnounceArgs struct {
	features.ServiceArgs
	Announcement connectors.Announcement
}

type KeyArgs struct {
	features.ServiceArgs
	Key string
}

type AnnouncementsArgs struct {
	features.ServiceArgs
}

type AnnouncementsResponse struct {
	Common        features.ServiceResponse
	Announcements []connectors.Announcement
}

type PutDiscoveredArgs struct {
	features.ServiceArgs
	Connection string
	Contract   connectors.Contract
}

type PutDiscoveredResponse struct {
	Common     features.ServiceResponse
	Discovered connectors.Discovered
}

type LedgerPutArgs struct {
	features.ServiceArgs
	Connection string
	Operation  string
	Key        string
	Entry      connectors.LedgerEntry
}

type TryLockArgs struct {
	features.ServiceArgs
	Connection string
	Holder     string
}

type TryLockResponse struct {
	Common features.ServiceResponse
	Held   bool
	Rev    uint64
}

type UnlockArgs struct {
	features.ServiceArgs
	Connection string
	Rev        uint64
}

type SourcesArgs struct {
	features.ServiceArgs // its context ends the watch
}

// SourcesRequest delivers the updates; Updates closes when the watch ends.
type SourcesRequest struct {
	Updates <-chan connectors.SourceUpdate
}

type LeaseArgs struct {
	features.ServiceArgs
	Key   string
	Lease connectors.Lease
	Rev   uint64
}

type LeaseResponse struct {
	Common features.ServiceResponse
	Rev    uint64
}

type CursorResponse struct {
	Common features.ServiceResponse
	Cursor connectors.Cursor
	Found  bool
}

type PutCursorArgs struct {
	features.ServiceArgs
	Key    string
	Cursor connectors.Cursor
}

type PublishArgs struct {
	features.ServiceArgs
	Connection string
	Contract   string
	Event      string
	ID         string
	Payload    json.RawMessage
	At         time.Time
}
