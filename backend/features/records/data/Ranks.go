package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Ranks (forge 30a, Records.md › Built in forge 30a): a rank field is an
// ordered position, set only by record.move, which computes a rank between
// the two neighbours it names, by the Rank between node in a graph that
// makes records, and on create or import; record.update refuses it. The
// ranks follower sends record.rebalance when a rank grows past
// records.RankRebalanceAt characters.

// Ranker is the follower that rebalances ranks that grew too long.
const Ranker = "records.ranks"

// MaxRebalance is how many records record.rebalance rewrites in one batch;
// a scope holding more is left as it is (its ranks still order it).
const MaxRebalance = 900

func moveSpec() spec {
	return spec{
		id: "record.move", title: "Move a record", action: "record.update", events: []string{EventUpdated},
		description: "Moves a record to a new place in an order: sets its rank field to a rank between the record it goes after and the one it goes before (leave after out for the start, before out for the end), with any other values in set, such as a board column's status, in the same change. The neighbours need not still be next to each other: the record then still goes between them, so two people moving at once both keep their moves.",
		args: `{"type":"object","properties":{` + entityArg + `,"field":{"type":"string","description":"The rank field's id"},` +
			`"after":{"type":"string","description":"The id of the record it goes after; left out, it goes first"},` +
			`"before":{"type":"string","description":"The id of the record it goes before; left out, it goes last"},` +
			`"set":{"type":"object","description":"Other field id → new value, changed with the move (such as the status of the column it is dropped in)"},` + expectArg + `},"required":["entity","field"]}`,
		example: `{"entity":"task","field":"position","after":"7340032001","before":"7340032002","set":{"status":"doing"}}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := decode[struct {
				Field, After, Before string
				Set                  map[string]json.RawMessage
				Expect               uint64
			}](c.Args)
			if problems == nil {
				problems = c.expect(struct{ Expect uint64 }{a.Expect})
			}
			if problems != nil {
				return nil, problems
			}
			f, problems := c.field(a.Field, "args.field")
			if problems != nil {
				return nil, problems
			}
			if f.Type != "rank" {
				return nil, problem("args.field", fmt.Sprintf("%s is a %s field, not a position (rank)", f.Name, f.Type), "move records by a rank field; describe the entity type for its fields")
			}
			after, problems := c.neighbour(f, a.After, "args.after")
			if problems != nil {
				return nil, problems
			}
			before, problems := c.neighbour(f, a.Before, "args.before")
			if problems != nil {
				return nil, problems
			}
			rank, err := records.RankAmong(after, before)
			if err != nil {
				return nil, problem("", err.Error(), "")
			}
			change := Change{Values: map[string]json.RawMessage{}, Old: map[string]json.RawMessage{}}
			for id, v := range a.Set {
				if id == f.ID {
					problems = append(problems, live.Problem{Path: "args.set." + id, Message: f.Name + " is the field being moved", Fix: "leave it out of set"})
					continue
				}
				out, ps := c.value(id, v, "args.set."+id)
				if ps == nil {
					ps = c.notRank(id, "args.set."+id)
				}
				problems = append(problems, ps...)
				if ps == nil && !same(out, c.record[id]) {
					change.Values[id] = orNull(out)
					change.Old[id] = orNull(c.record[id])
				}
			}
			if problems != nil {
				return nil, problems
			}
			change.Values[f.ID] = raw(rank)
			change.Old[f.ID] = orNull(c.record[f.ID])
			return []live.NewEvent{{Type: EventUpdated, Data: change}}, nil
		},
	}
}

func rebalanceSpec() spec {
	return spec{
		id: "record.rebalance", title: "Rebalance positions", action: "record.update", internal: true, events: []string{EventUpdated},
		description: "Writes the ranks of one scope (the records whose scope field holds one value) again, evenly and short, in their order, in one batch; sent by the ranks follower when a rank grows too long.",
		args:        `{"type":"object","properties":{` + entityArg + `,"field":{"type":"string"},"scope":{"description":"The scope field's value; null for records without one"}},"required":["entity","field"]}`,
		example:     `{"entity":"task","field":"position","scope":"doing"}`,
		decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
			a, problems := decode[struct {
				Field string
				Scope json.RawMessage
			}](c.Args)
			if problems != nil {
				return nil, problems
			}
			f, problems := c.field(a.Field, "args.field")
			if problems != nil || f.Type != "rank" {
				return nil, problems
			}
			q := Query{Entity: c.entity.ID, Sort: []Sort{{Field: f.ID}}, Fields: []string{f.ID}, Limit: MaxRebalance + 1}
			if f.Scope != "" {
				q.Filter = &Filter{Field: f.Scope, Value: orNull(a.Scope)}
			}
			args := c.ServiceArgs
			args.Principal = features.ServicePrincipal(c.Principal.TenantID)
			res, err := Find(args, c.deps, q)
			if err != nil {
				return nil, problem("", "the scope cannot be read: "+err.Error(), "")
			}
			if len(res.Records) > MaxRebalance {
				return nil, nil
			}
			ranks := records.EvenRanks(len(res.Records))
			var events []live.NewEvent
			for i, r := range res.Records {
				var id string
				_ = json.Unmarshal(r[MetaID], &id)
				v := raw(ranks[i])
				if same(v, r[f.ID]) {
					continue
				}
				t := live.Target{Area: live.AreaRecords, Type: c.entity.ID, ID: id}
				ev := live.NewEvent{Type: EventUpdated, Data: Change{Values: map[string]json.RawMessage{f.ID: v}, Old: map[string]json.RawMessage{f.ID: orNull(r[f.ID])}}}
				if t != c.Target {
					if _, err := c.readTarget(t); err != nil {
						return nil, problem("", "the record cannot be read: "+err.Error(), "")
					}
					ev.Target = &t
				}
				events = append(events, ev)
			}
			return events, nil
		},
	}
}

// neighbour is the rank of the record id a move names beside it ("" when
// none is named, or it has no rank yet).
func (c *decision) neighbour(f records.Field, id, path string) (string, []live.Problem) {
	if id == "" {
		return "", nil
	}
	if id == c.Target.ID {
		return "", problem(path, "a record cannot move beside itself", "name the record next to where it goes")
	}
	if !live.ValidToken(id) {
		return "", problem(path, fmt.Sprintf("%q is not a record id", id), "")
	}
	state, err := c.readTarget(live.Target{Area: live.AreaRecords, Type: c.Target.Type, ID: id})
	if err != nil {
		return "", problem("", "the record cannot be read: "+err.Error(), "")
	}
	var r Record
	_ = json.Unmarshal(state, &r)
	if r == nil || !records.Allows(first(c.entity.RuleFor(c.Context(), c.Principal, "read")), r, c.Principal) {
		return "", problem(path, fmt.Sprintf("there is no %s record %q", c.Target.Type, id), "name a record shown next to where it goes")
	}
	var rank string
	_ = json.Unmarshal(r[f.ID], &rank)
	return rank, nil
}

// notRank refuses setting a rank field by hand: it is set by record.move.
func (c *decision) notRank(id, path string) []live.Problem {
	if f, _, ok := c.entity.FieldByID(id); ok && f.Type == "rank" {
		return problem(path, f.Name+" is a position, set by moving the record", fmt.Sprintf(`send record.move {entity: %q, field: %q, after: "<id of the record before>", before: "<id of the record after>"}`, c.entity.ID, id))
	}
	return nil
}

// followRanks starts the ranks follower in the principal's tenant.
func followRanks(args features.ServiceArgs, deps Deps) error {
	tenant := args.Principal.TenantID
	subject := fmt.Sprintf("evt.%d.%s.*.*.%s", tenant, live.AreaRecords, EventUpdated)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaRecords, Ranker, []string{subject}, func(e live.Event) error {
		return rebalanceRanks(deps, e)
	})
}

// rebalanceRanks sends record.rebalance for each rank an update made too
// long, its id naming the event and field, so a redelivered event sends
// nothing new.
func rebalanceRanks(deps Deps, e live.Event) error {
	var ch Change
	if json.Unmarshal(e.Data, &ch) != nil {
		return nil
	}
	var long []string
	for id, v := range ch.Values {
		if len(v) > records.RankRebalanceAt+2 && bytes.HasPrefix(v, []byte(`"`)) {
			long = append(long, id)
		}
	}
	if len(long) == 0 {
		return nil
	}
	ctx := context.Background()
	types, err := PublishedTypes(ctx, deps, e.TenantID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(types, func(t records.EntityType) bool { return t.ID == e.Target.Type })
	if i < 0 {
		return nil
	}
	if err := WaitFolded(ctx, deps, e.TenantID, e.Seq); err != nil {
		return err
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(e.TenantID)}
	for _, id := range long {
		f, _, ok := types[i].FieldByID(id)
		if !ok || f.Type != "rank" {
			continue
		}
		r, _, err := Get(args, deps, e.Target.Type, e.Target.ID)
		if err != nil || r == nil {
			return err
		}
		a := map[string]any{"entity": e.Target.Type, "field": f.ID}
		if f.Scope != "" {
			a["scope"] = orNull(r[f.Scope])
		}
		b, _ := json.Marshal(a)
		resp := deps.Bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("rebalance-%d-%s", e.Seq, strings.ReplaceAll(f.ID, ".", "_")), Type: "record.rebalance", Target: e.Target.ID, Args: b})
		if resp.Common.Code >= 300 && resp.Common.Code != 422 {
			return fmt.Errorf("cannot rebalance %s %s: %s", e.Target.Type, f.ID, resp.Common.Message)
		}
	}
	return nil
}
