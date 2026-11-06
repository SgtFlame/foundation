package prefabservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// Service serves the published versions of prefabs, as the tenant
// database keeps them (documentation/ServerUI.md › Built in slice 25), for
// the browser's lists outside a subscription and for export. Drafts are
// edited by the prefab.* commands and read by the prefab.describe and
// prefab.list queries.
type Service interface {
	ListPrefabs(args ListPrefabsArgs) ListPrefabsResponse
	GetPrefab(args GetPrefabArgs) GetPrefabResponse
	ListVersions(args ListVersionsArgs) ListVersionsResponse
	// Preview evaluates a prefab once (forge 04b), as the prefab.preview
	// tool does.
	Preview(args PreviewArgs) PreviewResponse
}

// VersionInfo is a published version without its definition.
type VersionInfo struct {
	PrefabID    string
	Version     int
	Name        string
	Owner       string
	PublishedBy uint64
	PublishedAt time.Time
}

type ListPrefabsArgs struct {
	features.ServiceArgs
}

type ListPrefabsResponse struct {
	Common  features.ServiceResponse
	Prefabs []VersionInfo // each prefab's last published version, by name
}

// GetPrefabArgs names a published version; Version 0 is the last.
type GetPrefabArgs struct {
	features.ServiceArgs
	ID      string
	Version int
}

type GetPrefabResponse struct {
	Common features.ServiceResponse
	Info   VersionInfo
	Prefab ui.Prefab
}

type ListVersionsArgs struct {
	features.ServiceArgs
	ID string
}

type ListVersionsResponse struct {
	Common   features.ServiceResponse
	Versions []VersionInfo // newest first
}

// PreviewArgs name the prefab to evaluate and its props; nil props take
// its preview data. Draft evaluates its draft, else its last published
// version.
type PreviewArgs struct {
	features.ServiceArgs
	Prefab string
	Props  map[string]any
	Draft  bool
}

type PreviewResponse struct {
	Common   features.ServiceResponse
	Tree     *ui.Node     // primitives only, as a browser gets it
	Problems []ui.Problem // each at its node's key path
}
