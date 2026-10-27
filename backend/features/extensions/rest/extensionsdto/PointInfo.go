package extensionsdto

import "github.com/SgtFlame/foundation/backend/features/extensions"

type PointInfo struct {
	ID            string         `json:"id"`
	Description   string         `json:"description"`
	Contributions []Contribution `json:"contributions"`
}

type Contribution struct {
	ID     string `json:"id"`
	Order  int    `json:"order"`
	Source string `json:"source,omitempty"`
}

func PointInfoFrom(p extensions.PointInfo) PointInfo {
	out := PointInfo{ID: p.ID, Description: p.Description, Contributions: make([]Contribution, len(p.Contributions))}
	for i, c := range p.Contributions {
		out.Contributions[i] = Contribution{ID: c.ID, Order: c.Order, Source: c.Source}
	}
	return out
}
