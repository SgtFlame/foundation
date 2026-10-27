package chronicledto

import "github.com/SgtFlame/foundation/backend/features/chronicle"

// Link relates two entries; it has no direction.
type Link struct {
	ID     uint64 `json:"id,string"`
	FromID uint64 `json:"fromId,string"`
	ToID   uint64 `json:"toId,string"`
	Kind   string `json:"kind"`
}

func LinkFrom(l chronicle.EntryLink) Link {
	return Link{ID: l.ID, FromID: l.FromID, ToID: l.ToID, Kind: l.Kind}
}

func linksFrom(links []chronicle.EntryLink) []Link {
	out := make([]Link, len(links))
	for i, l := range links {
		out[i] = LinkFrom(l)
	}
	return out
}
