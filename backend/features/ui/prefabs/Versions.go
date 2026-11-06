package prefabs

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"gorm.io/gorm"
)

// Follower is the durable consumer that copies published versions into
// the tenant database.
const Follower = "ui.prefabVersions"

// Follow makes sure the principal's tenant copies its published prefabs
// into the tenant database (ui_prefabs, ui_prefab_versions). The copy runs
// as a follower of the def stream: prefab.publish starts it before its
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

// copyVersion keeps one published event as a version row and as the
// prefab's last version; a redelivered event finds its rows and changes
// nothing.
func copyVersion(deps Deps, e live.Event) error {
	p, err := decode[Publication](e.Data)
	if err != nil {
		return fmt.Errorf("cannot read the publication: %w", err)
	}
	p.Prefab.ID, p.Prefab.Version = e.Target.ID, p.Version
	def, _ := json.Marshal(p.Prefab)
	return deps.DB.Transaction(func(tx *gorm.DB) error {
		row := ui.PrefabVersion{Tenanted: db.Tenanted{TenantID: e.TenantID}, PrefabID: e.Target.ID, Version: p.Version}
		err := tx.Where(row).Attrs(ui.PrefabVersion{
			Name: p.Prefab.Name, Owner: p.Prefab.Owner, Definition: string(def),
			PublishedBy: e.UserID, PublishedAt: e.Time, Stream: e.Stream, Seq: e.Seq,
		}).FirstOrCreate(&row).Error
		if err != nil {
			return err
		}
		last := ui.PrefabRow{Tenanted: db.Tenanted{TenantID: e.TenantID}, PrefabID: e.Target.ID}
		if err := tx.Where(last).FirstOrCreate(&last).Error; err != nil {
			return err
		}
		if last.Version >= p.Version {
			return nil
		}
		return tx.Model(&last).Updates(map[string]any{"version": p.Version, "name": p.Prefab.Name, "owner": p.Prefab.Owner,
			"definition": string(def), "published_by": e.UserID, "published_at": e.Time}).Error
	})
}
