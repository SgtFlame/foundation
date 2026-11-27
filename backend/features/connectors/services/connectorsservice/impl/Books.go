package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/services/connectorsservice"
)

func (s *service) Announce(args connectorsservice.AnnounceArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, s.books.Announce(args.Context(), args.Announcement))}
}

func (s *service) Withdraw(args connectorsservice.KeyArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, s.books.Withdraw(args.Context(), args.Key))}
}

func (s *service) Announcements(args connectorsservice.AnnouncementsArgs) connectorsservice.AnnouncementsResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.AnnouncementsResponse{Common: *f}
	}
	all, err := s.books.Announcements(args.Context())
	return connectorsservice.AnnouncementsResponse{Common: s.done(args.ServiceArgs, err), Announcements: all}
}

func (s *service) PutDiscovered(args connectorsservice.PutDiscoveredArgs) connectorsservice.PutDiscoveredResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.PutDiscoveredResponse{Common: *f}
	}
	d, err := s.books.PutDiscovered(args.Context(), args.Principal.TenantID, args.Connection, args.Contract)
	return connectorsservice.PutDiscoveredResponse{Common: s.done(args.ServiceArgs, err), Discovered: d}
}

func (s *service) LedgerPut(args connectorsservice.LedgerPutArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, s.books.LedgerPut(args.Context(), args.Principal.TenantID, args.Connection, args.Operation, args.Key, args.Entry))}
}

func (s *service) TryLock(args connectorsservice.TryLockArgs) connectorsservice.TryLockResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.TryLockResponse{Common: *f}
	}
	rev, held, err := connectors.TryLock(args.Context(), s.js, args.Principal.TenantID, args.Connection, args.Holder)
	return connectorsservice.TryLockResponse{Common: s.done(args.ServiceArgs, err), Held: held, Rev: rev}
}

func (s *service) Unlock(args connectorsservice.UnlockArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, connectors.Unlock(args.Context(), s.js, args.Principal.TenantID, args.Connection, args.Rev))}
}

// Sources sends the sources and their changes until the args' context
// ends; Updates closes then, or at once when the watch cannot start.
func (s *service) Sources(args connectorsservice.SourcesArgs) *connectorsservice.SourcesRequest {
	if f := s.check(args.ServiceArgs); f != nil {
		empty := make(chan connectors.SourceUpdate)
		close(empty)
		return &connectorsservice.SourcesRequest{Updates: empty}
	}
	updates, err := s.books.Sources(args.Context())
	if err != nil {
		log.Error(args.Context(), err, "cannot watch the event sources")
		empty := make(chan connectors.SourceUpdate)
		close(empty)
		return &connectorsservice.SourcesRequest{Updates: empty}
	}
	return &connectorsservice.SourcesRequest{Updates: updates}
}

func (s *service) lease(args connectorsservice.LeaseArgs, rev uint64, err error) connectorsservice.LeaseResponse {
	switch {
	case errors.Is(err, connectors.ErrLeaseTaken):
		return connectorsservice.LeaseResponse{Common: *args.Fail(http.StatusConflict, log.Code("lease_taken").New("the lease is held"))}
	case errors.Is(err, connectors.ErrLeaseLost):
		return connectorsservice.LeaseResponse{Common: *args.Fail(http.StatusConflict, log.Code("lease_lost").New("the lease was lost"))}
	}
	return connectorsservice.LeaseResponse{Common: s.done(args.ServiceArgs, err), Rev: rev}
}

func (s *service) LeaseCreate(args connectorsservice.LeaseArgs) connectorsservice.LeaseResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.LeaseResponse{Common: *f}
	}
	rev, err := s.books.LeaseCreate(args.Context(), args.Key, args.Lease)
	return s.lease(args, rev, err)
}

func (s *service) LeaseUpdate(args connectorsservice.LeaseArgs) connectorsservice.LeaseResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.LeaseResponse{Common: *f}
	}
	rev, err := s.books.LeaseUpdate(args.Context(), args.Key, args.Lease, args.Rev)
	return s.lease(args, rev, err)
}

func (s *service) LeaseDelete(args connectorsservice.LeaseArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, s.books.LeaseDelete(args.Context(), args.Key, args.Rev))}
}

func (s *service) Cursor(args connectorsservice.KeyArgs) connectorsservice.CursorResponse {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.CursorResponse{Common: *f}
	}
	c, ok, err := s.books.Cursor(args.Context(), args.Key)
	return connectorsservice.CursorResponse{Common: s.done(args.ServiceArgs, err), Cursor: c, Found: ok}
}

func (s *service) PutCursor(args connectorsservice.PutCursorArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, s.books.PutCursor(args.Context(), args.Key, args.Cursor))}
}

func (s *service) Publish(args connectorsservice.PublishArgs) connectorsservice.Response {
	if f := s.check(args.ServiceArgs); f != nil {
		return connectorsservice.Response{Common: *f}
	}
	err := s.books.Publish(args.Context(), args.Principal.TenantID, args.Connection, args.Contract, args.Event, args.ID, args.Payload, args.At)
	var ce *connectors.Error
	if errors.As(err, &ce) {
		return connectorsservice.Response{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_event").New(ce.Message))}
	}
	return connectorsservice.Response{Common: s.done(args.ServiceArgs, err)}
}
