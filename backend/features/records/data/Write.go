package data

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Writes are the record commands a command graph's record nodes decide
// beside their own command: record.create, update, delete, addTo and
// removeFrom.
var Writes = []string{"record.create", "record.update", "record.delete", "record.addTo", "record.removeFrom"}

// Write decides record command typ on another record than d's target, for
// a command that changes several records (a command graph's record nodes,
// forge 03i): it checks the principal may run typ on the entity type and
// the args against typ's schema, reads the record with d.Read (so the
// commit expects it unchanged, and every event commits together or none
// does), decides as typ itself would, and returns the events with their
// target set. The args name the entity, as typ's do; a create without an
// id gets a new one. Without d.Read (a test run) the record is read from
// the projection.
func Write(d live.Decision, deps Deps, typ, id string, args json.RawMessage) ([]live.NewEvent, string, []live.Problem) {
	var s *spec
	for _, x := range commandSpecs() {
		if x.id == typ && !x.internal {
			s = &x
		}
	}
	if s == nil {
		return nil, "", problem("", fmt.Sprintf("%q is not a record command", typ), "use one of record.create, update, delete, addTo or removeFrom")
	}
	var a struct{ Entity string }
	_ = json.Unmarshal(args, &a)
	if !live.ValidToken(a.Entity) {
		return nil, "", problem("entity", fmt.Sprintf("%q is not an entity type id", a.Entity), "list the entity types with entity.list")
	}
	if id == "" && s.create {
		id = strconv.FormatUint(features.NewTSID(), 10)
	}
	if !live.ValidToken(id) {
		return nil, "", problem("id", fmt.Sprintf("%q is not a record id", id), "")
	}
	resource := records.Resource(d.Principal, a.Entity)
	if err := authz.Check(d.Context(), d.Principal, s.action, resource).Err(s.action, resource); err != nil {
		return nil, "", []live.Problem{{Code: "forbidden", Path: "entity", Message: err.Error()}}
	}
	if err := tools.CheckArgs(json.RawMessage(s.args), args); err != nil {
		return nil, "", problem("args", err.Error(), "")
	}
	t := live.Target{Area: live.AreaRecords, Type: a.Entity, ID: id}
	var state json.RawMessage
	if d.Read != nil {
		v, err := d.Read(Projector, t)
		if err != nil {
			return nil, "", problem("", "the record cannot be read: "+err.Error(), "")
		}
		state = v
	} else if deps.Projections != nil {
		v, ok, err := deps.Projections.Get(d.Context(), d.Principal.TenantID, Projector, t.Key())
		if err != nil {
			return nil, "", problem("", "the record cannot be read: "+err.Error(), "")
		}
		if ok {
			state = v.Value
		}
	}
	sub := d
	sub.Target, sub.State, sub.Args = t, state, args
	events, problems := decideRecord(*s, sub, deps)
	if problems != nil {
		return nil, id, problems
	}
	for i := range events {
		if events[i].Target == nil { // an autoNumber counter has its own
			events[i].Target = &t
		}
	}
	return events, id, nil
}

// Get reads one record once, as records.get gives it: nil when there is
// none or the entity type's read rule hides it from the principal. pos is
// the record's position.
func Get(args features.ServiceArgs, deps Deps, entity, id string) (Record, live.Position, error) {
	e, problems := Entity(args, deps.Projections, entity)
	if problems != nil {
		return nil, nil, log.Code("entity_not_found").Errorf("%s", problems[0].Message)
	}
	if !live.ValidToken(id) {
		return nil, live.Position{}, nil
	}
	v, ok, err := deps.Projections.Get(args.Context(), args.Principal.TenantID, Projector, Key(entity, id))
	if err != nil || !ok || v.Value == nil {
		return nil, live.Position{}, err
	}
	var r Record
	if err := json.Unmarshal(v.Value, &r); err != nil {
		return nil, nil, log.Wrap(err)
	}
	if !records.Allows(first(e.RuleFor(args.Context(), args.Principal, "read")), r, args.Principal) {
		return nil, v.Position(), nil
	}
	return r, v.Position(), nil
}
