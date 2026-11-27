package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
)

// The memory operations serve chronicle.Memory to steps in other
// processes, as MemoryIn does in this one.

func (s *service) memory() chronicle.Memory { return chronicle.MemoryIn(s.db) }

func (s *service) MemoryEntries(args chronicleservice.MemoryEntriesArgs) chronicleservice.MemoryEntriesResponse {
	if f := check(args.ServiceArgs, "chronicle.read", 0); f != nil {
		return chronicleservice.MemoryEntriesResponse{Common: *f}
	}
	entries, err := s.memory().Entries(args.ServiceArgs, args.Visible, args.Statuses)
	if err != nil {
		return chronicleservice.MemoryEntriesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.MemoryEntriesResponse{Common: features.OK(), Entries: entries}
}

func (s *service) MemoryLinks(args chronicleservice.MemoryLinksArgs) chronicleservice.MemoryLinksResponse {
	if f := check(args.ServiceArgs, "chronicle.read", 0); f != nil {
		return chronicleservice.MemoryLinksResponse{Common: *f}
	}
	links, err := s.memory().Links(args.ServiceArgs, args.Hubs)
	if err != nil {
		return chronicleservice.MemoryLinksResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.MemoryLinksResponse{Common: features.OK(), Links: links}
}

func (s *service) MemoryActive(args chronicleservice.MemoryActiveArgs) chronicleservice.MemoryActiveResponse {
	if f := check(args.ServiceArgs, "chronicle.read", 0); f != nil {
		return chronicleservice.MemoryActiveResponse{Common: *f}
	}
	ids, err := s.memory().Active(args.ServiceArgs, args.RoundID)
	if err != nil {
		return chronicleservice.MemoryActiveResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.MemoryActiveResponse{Common: features.OK(), Entries: ids}
}

func (s *service) MemoryApply(args chronicleservice.MemoryApplyArgs) chronicleservice.MemoryApplyResponse {
	if f := check(args.ServiceArgs, "chronicle.write", 0); f != nil {
		return chronicleservice.MemoryApplyResponse{Common: *f}
	}
	updated, proposed, err := s.memory().Apply(args.ServiceArgs, args.Update)
	if err != nil {
		return chronicleservice.MemoryApplyResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.MemoryApplyResponse{Common: features.OK(), Updated: updated, Proposed: proposed}
}

func (s *service) MemoryFlag(args chronicleservice.MemoryFlagArgs) chronicleservice.MemoryFlagResponse {
	if f := check(args.ServiceArgs, "chronicle.write", 0); f != nil {
		return chronicleservice.MemoryFlagResponse{Common: *f}
	}
	if err := s.memory().Flag(args.ServiceArgs, args.RoundID); err != nil {
		return chronicleservice.MemoryFlagResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return chronicleservice.MemoryFlagResponse{Common: features.OK()}
}
