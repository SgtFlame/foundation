// Package appservice exports a published app as one JSON document,
// checks a document, and imports one into a tenant (forge 05e,
// documentation/Apps.md › Built in slice 35). Everything else about apps
// is commands and queries on the live connection (apps/manifests,
// apps/installs).
package appservice

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/live"
)

type Service interface {
	// Export writes a published version as a document, refused with the
	// problems of the check when it fails it.
	Export(args ExportArgs) ExportResponse
	// Check is validation of a document plus the blueprints-only check,
	// against this server's primitives.
	Check(args CheckArgs) CheckResponse
	// Import recreates a document in the principal's tenant as a draft and
	// a published version, ready to install. A part the tenant already has
	// is imported only when the caller decides it (Overwrite or Keep);
	// until every one is decided, nothing is imported and the plan says
	// which ones to ask about.
	Import(args ImportArgs) ImportResponse
}

type ExportArgs struct {
	features.ServiceArgs
	App     string
	Version int  // 0 is the last published
	Seed    bool // include the records of the app's own entity types
}

type ExportResponse struct {
	Common   features.ServiceResponse
	Document *apps.Document
	Problems []live.Problem
}

type CheckArgs struct {
	features.ServiceArgs
	Document apps.Document
}

type CheckResponse struct {
	Common   features.ServiceResponse
	Problems []live.Problem
}

type ImportArgs struct {
	features.ServiceArgs
	Document apps.Document
	// ID is the app's id here; the document's when empty. The app's
	// command and query graphs (cmd-<app>-…, qry-<app>-…) follow it.
	ID string
	// Overwrite and Keep decide the parts the tenant already has, each
	// named <type>:<id> (prefab:recordTable): overwrite imports the
	// document's, keep leaves the tenant's.
	Overwrite []string
	Keep      []string
	DryRun    bool // only the plan
}

type ImportResponse struct {
	Common   features.ServiceResponse
	Plan     Plan
	Imported bool
	Version  int // the version published here
	Problems []live.Problem
}

// Plan is what an import does: the app's id here and each part.
type Plan struct {
	App   string     `json:"app"`
	Parts []PartPlan `json:"parts"`
}

// Part statuses in a plan.
const (
	PartNew       = "new"       // the tenant has no part of that id
	PartSame      = "same"      // the tenant has it, published as the document has it
	PartDuplicate = "duplicate" // the tenant has a different one: overwrite or keep
)

// Decisions on a duplicate part.
const (
	DecideOverwrite = "overwrite"
	DecideKeep      = "keep"
)

// PartPlan is one part of a plan, with both sides' versions and dates to
// decide a duplicate by (forge 05e question 2).
type PartPlan struct {
	Type     string       `json:"type"` // prefab, graph or entity
	ID       string       `json:"id"`
	Group    string       `json:"group"` // parts or tenantParts
	Name     string       `json:"name"`
	Status   string       `json:"status"`
	Decision string       `json:"decision,omitempty"`
	Document PartVersion  `json:"document"`
	Target   *PartVersion `json:"target,omitempty"`
}

// PartVersion is one side of a part: its owner, last published version
// and when it was published or last changed.
type PartVersion struct {
	Owner       string    `json:"owner,omitempty"`
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
	Changed     bool      `json:"changed,omitempty"` // the target's draft has changes not published
}
