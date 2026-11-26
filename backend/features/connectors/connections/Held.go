package connections

import (
	"context"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// Connections held by an app's records (forge 07n question 4). A record of
// one of an installed app's own entity types that holds a connection in a
// connection field lets the app's principal use it, until no such record
// holds it: the Mail app syncs and sends through the connections its
// accounts hold, a user's own included. Whoever sets the field must be
// able to use the connection (records' Deps.UseConnection, wired to
// CheckUse), so only someone who may use it lends it to the app.

// Held are the connections the app's own records hold, by id. It reads the
// app's installed entity types with connection fields and their records
// from the projections, every time: a per-call scan of what are, for the
// apps that do this, a few account records.
func Held(ctx context.Context, d Deps, tenant uint64, app string) (map[string]bool, error) {
	out := map[string]bool{}
	if d.Projections == nil || app == "" {
		return out, nil
	}
	in, ok := installs.Running(ctx, d.Projections, tenant, app)
	if !ok || len(in.Version.Manifest.Entities) == 0 {
		return out, nil
	}
	rd := data.Deps{Projections: d.Projections}
	types, err := data.PublishedTypes(ctx, rd, tenant)
	if err != nil {
		return nil, err
	}
	for _, e := range types {
		if !ownEntity(in.Version.Manifest.Entities, e.ID) {
			continue
		}
		fields := connectionFields(e)
		if len(fields) == 0 {
			continue
		}
		recs, err := data.RecordsOf(rd, tenant, e.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			for _, f := range fields {
				var id string
				if json.Unmarshal(r[f], &id) == nil && id != "" {
					out[id] = true
				}
			}
		}
	}
	return out, nil
}

func ownEntity(entities []string, id string) bool {
	for _, x := range entities {
		if x == id {
			return true
		}
	}
	return false
}

// connectionFields are an entity type's visible connection fields' ids.
func connectionFields(e records.EntityType) []string {
	var out []string
	for _, f := range e.Visible() {
		if f.Type == "connection" {
			out = append(out, f.ID)
		}
	}
	return out
}

// HoldsConnections reports whether an entity type has a connection field,
// so a change to its records may change what an app may use.
func HoldsConnections(e records.EntityType) bool { return len(connectionFields(e)) > 0 }

// CheckUse checks that the principal may use the connection: CheckAccess's
// use, or, for an app's principal, a record of the app's own types holding
// it (Held).
func (d Deps) CheckUse(args features.ServiceArgs, c Connection) error {
	err := CheckAccess(args, c, "use")
	if err == nil || args.Principal.Kind != features.PrincipalApp {
		return err
	}
	held, herr := Held(args.Context(), d, args.Principal.TenantID, args.Principal.App)
	if herr == nil && held[c.ID] {
		return nil
	}
	return err
}

// UseConnection is records' Deps.UseConnection: the principal may use the
// connection a record command sets on a connection field.
func UseConnection(d func() Deps) func(args features.ServiceArgs, id string) error {
	return func(args features.ServiceArgs, id string) error {
		deps := d()
		if deps.Projections == nil {
			return nil
		}
		c, ok, err := Get(args.Context(), deps.Projections, args.Principal.TenantID, id)
		if err != nil {
			return err
		}
		if !ok {
			return log.Code("connection_not_found").Errorf("there is no connection %s", id)
		}
		return deps.CheckUse(args, c)
	}
}
