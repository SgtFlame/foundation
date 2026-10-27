package secretsdto

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/secrets/services/secretsservice"
)

// Secret describes a secret; it never carries a value.
type Secret struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title,omitempty"`
	Set       bool      `json:"set"`
	Version   int       `json:"version,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	Source    string    `json:"source,omitempty"`
	Protected bool      `json:"protected,omitempty"`
}

func SecretFrom(s secretsservice.Secret) Secret {
	return Secret{Name: s.Name, Kind: s.Kind, Title: s.Title, Set: s.Set, Version: s.Version, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy, Source: s.Source, Protected: s.Protected}
}
