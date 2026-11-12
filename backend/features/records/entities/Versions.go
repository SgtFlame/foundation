package entities

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Follower is the durable consumer that copies published versions into
// the tenant database.
const Follower = "records.entityVersions"

// Follow makes sure the principal's tenant copies its published entity
// types into the tenant database (records_entity_versions). The copy runs
// as a follower of the def stream: entity.publish starts it before its
// event commits, and reading the versions starts it after a restart,
// catching up on what was published meanwhile.
func Follow(args features.ServiceArgs, deps Deps) error {
	if deps.DB == nil {
		return nil
	}
	tenant := args.Principal.TenantID
	subject := live.Target{Area: live.AreaDefinitions, Type: TargetType, ID: "*"}.Subject(tenant, EventPublished)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, Follower, []string{subject}, func(e live.Event) error {
		return copyVersion(deps, e)
	})
}

// copyVersion keeps one published event as a row; a redelivered event
// finds its row and changes nothing.
func copyVersion(deps Deps, e live.Event) error {
	p, err := decode[Publication](e.Data)
	if err != nil {
		return fmt.Errorf("cannot read the publication: %w", err)
	}
	p.Entity.ID, p.Entity.Version = e.Target.ID, p.Version
	def, _ := json.Marshal(p.Entity)
	row := records.EntityTypeVersion{Tenanted: db.Tenanted{TenantID: e.TenantID}, EntityID: e.Target.ID, Version: p.Version}
	return deps.DB.Where(row).Attrs(records.EntityTypeVersion{
		Name: p.Entity.Name, Owner: p.Entity.Owner, Definition: string(def),
		PublishedBy: e.UserID, PublishedAt: e.Time, Stream: e.Stream, Seq: e.Seq,
	}).FirstOrCreate(&row).Error
}
