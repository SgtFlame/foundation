package entityservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Service serves the published versions of entity types, as the tenant
// database keeps them (documentation/Records.md › Built in slice 16), for
// export and for clients outside the live connection. Drafts are edited by
// the entity.* commands and read by the entity.describe and entity.list
// queries.
type Service interface {
	ListEntityTypes(args ListEntityTypesArgs) ListEntityTypesResponse
	GetEntityType(args GetEntityTypeArgs) GetEntityTypeResponse
	ListVersions(args ListVersionsArgs) ListVersionsResponse
}

// VersionInfo is a published version without its definition.
type VersionInfo struct {
	EntityID    string
	Version     int
	Name        string
	Owner       string
	PublishedBy uint64
	PublishedAt time.Time
}

type ListEntityTypesArgs struct {
	features.ServiceArgs
}

type ListEntityTypesResponse struct {
	Common      features.ServiceResponse
	EntityTypes []VersionInfo // each entity type's last published version, by name
}

// GetEntityTypeArgs names a published version; Version 0 is the last.
type GetEntityTypeArgs struct {
	features.ServiceArgs
	ID      string
	Version int
}

type GetEntityTypeResponse struct {
	Common     features.ServiceResponse
	Info       VersionInfo
	EntityType records.EntityType
}

type ListVersionsArgs struct {
	features.ServiceArgs
	ID string
}

type ListVersionsResponse struct {
	Common   features.ServiceResponse
	Versions []VersionInfo // newest first
}
