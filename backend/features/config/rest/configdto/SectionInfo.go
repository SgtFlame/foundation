package configdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/config"
)

// SectionInfo is a backend section as the frontend registers it on its
// configuration point.
type SectionInfo struct {
	ID          string          `json:"id"`
	Order       int             `json:"order"`
	Source      string          `json:"source"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Category    []string        `json:"category"`
	Scopes      []string        `json:"scopes"`
	Schema      json.RawMessage `json:"schema"`
	Defaults    json.RawMessage `json:"defaults"`
	Version     int             `json:"version"`
}

func SectionInfoFrom(s config.Section) SectionInfo {
	d := s.Describe()
	return SectionInfo{
		ID: d.ID, Order: d.Order, Source: d.Source,
		Title: s.Title, Description: s.Description, Category: s.Category, Scopes: s.Scopes,
		Schema: s.Schema, Defaults: s.Defaults, Version: s.Version,
	}
}
