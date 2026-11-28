package chronicleservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Memory is a chronicle.Memory through svc (a proxy in a process without
// the tenant database, forge 21d-2), as the steps' principal.
func Memory(svc Service) chronicle.Memory { return memory{svc} }

type memory struct{ svc Service }

func fail(c features.ServiceResponse) error {
	if c.Code == 200 {
		return nil
	}
	return logging.Code(c.ErrorCode).Errorf("%s", c.Message)
}

func (m memory) Entries(args features.ServiceArgs, visible bool, statuses []string) ([]chronicle.Entry, error) {
	r := m.svc.MemoryEntries(MemoryEntriesArgs{ServiceArgs: args, Visible: visible, Statuses: statuses})
	return r.Entries, fail(r.Common)
}

func (m memory) Links(args features.ServiceArgs, hubs []uint64) ([]chronicle.EntryLink, error) {
	r := m.svc.MemoryLinks(MemoryLinksArgs{ServiceArgs: args, Hubs: hubs})
	return r.Links, fail(r.Common)
}

func (m memory) Active(args features.ServiceArgs, roundID uint64) ([]uint64, error) {
	r := m.svc.MemoryActive(MemoryActiveArgs{ServiceArgs: args, RoundID: roundID})
	return r.Entries, fail(r.Common)
}

func (m memory) Apply(args features.ServiceArgs, u chronicle.MemoryUpdate) ([]chronicle.Entry, []chronicle.Entry, error) {
	r := m.svc.MemoryApply(MemoryApplyArgs{ServiceArgs: args, Update: u})
	return r.Updated, r.Proposed, fail(r.Common)
}

func (m memory) Flag(args features.ServiceArgs, round uint64) error {
	return fail(m.svc.MemoryFlag(MemoryFlagArgs{ServiceArgs: args, RoundID: round}).Common)
}

func (m memory) Types(args features.ServiceArgs) ([]chronicle.EntryType, error) {
	r := m.svc.Types(TypesArgs{ServiceArgs: args})
	return r.Types, fail(r.Common)
}
