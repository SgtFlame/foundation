package data

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// UpsertID is the batch import command (forge 40c, question 2).
const UpsertID = "record.upsert"

// MaxUpsertRows is the most rows one record.upsert takes.
const MaxUpsertRows = 500

// contributeUpsert registers record.upsert: rows of values created, or,
// with match, each row updating the record whose match field holds the
// row's value. The whole batch is one command: it commits whole or not at
// all, and a retried command (same id) commits nothing twice. Imports and
// form submissions (forge 45b) share it.
func contributeUpsert(reg *extensions.Registry, deps func() Deps) {
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: UpsertID, Source: "records"}, Title: "Create or update records",
		Description: "Creates records from rows of values by field id, as one batch that commits whole or not at all (at most 500 rows; send more as several commands). With match (a field id, such as email), a row whose match value a record already holds updates that record instead, setting only the fields the row gives; rows with the same match value are merged, the later winning. " +
			"Each row is checked as record.create or record.update checks it (the principal needs those actions, the values their fields' types, required fields a value, the row rules); a refusal names each failing row as args.rows.<n>.<field>, so the caller can drop those rows and send the rest again. values alone is one row (a form submission). Match values are compared exactly as stored: normalize them (such as lowercase emails) first. Records the read rule hides from the principal are not matched.",
		Args: json.RawMessage(`{"type":"object","properties":{` + entityArg + `,` +
			`"match":{"type":"string","description":"The field id a row is matched on (an index on it keeps large types fast); every row is created when left out"},` +
			`"rows":{"type":"array","maxItems":500,"items":{"type":"object"},"description":"The rows: field id → value, at most 500"},` +
			`"values":{"type":"object","description":"One row, in place of rows: field id → value"}},"required":["entity"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"entity":"contact","match":"email","rows":[{"name":"Ada Lovelace","email":"ada@example.com"},{"name":"Bob Ross","email":"bob@example.com"}]}`)},
		Events:   []string{EventCreated, EventUpdated},
		Area:     live.AreaRecords, TargetType: "record", Projection: Projector, Action: "record.create",
		TypeOf: func(args json.RawMessage) (string, *live.Problem) {
			var a struct{ Entity string }
			_ = json.Unmarshal(args, &a)
			if !live.ValidToken(a.Entity) {
				return "", &live.Problem{Path: "args.entity", Message: fmt.Sprintf("%q is not an entity type id", a.Entity), Fix: "list the entity types with entity.list"}
			}
			return a.Entity, nil
		},
		Resource:  func(p features.Principal, t live.Target) string { return records.Resource(p, t.Type) },
		NewTarget: func() string { return strconv.FormatUint(features.NewTSID(), 10) },
		Holds:     func(d live.Decision) []live.Target { return counterOf(d, deps()) },
		Decide:    func(d live.Decision) ([]live.NewEvent, []live.Problem) { return decideUpsert(d, deps()) },
	})
}

type upsertArgs struct {
	Entity string
	Match  string
	Rows   []map[string]json.RawMessage
	Values map[string]json.RawMessage
}

// upsertGroup is the rows with one match value (or one row without match),
// merged in order, named by the first row's index.
type upsertGroup struct {
	row    int
	key    string
	values map[string]json.RawMessage
}

func decideUpsert(d live.Decision, deps Deps) ([]live.NewEvent, []live.Problem) {
	a, problems := decode[upsertArgs](d.Args)
	if problems != nil {
		return nil, problems
	}
	rows := a.Rows
	if a.Values != nil {
		rows = append([]map[string]json.RawMessage{a.Values}, rows...)
	}
	if len(rows) == 0 {
		return nil, problem("args.rows", "there are no rows to create or update", "give rows, or values for one")
	}
	if len(rows) > MaxUpsertRows {
		return nil, problem("args.rows", fmt.Sprintf("%d rows is more than the %d one command takes", len(rows), MaxUpsertRows), "send them in batches of 500")
	}
	e, problems := Entity(d.ServiceArgs, deps.Projections, a.Entity)
	if problems != nil {
		return nil, problems
	}
	// Rows without a match value are refused beside the other rows'
	// problems, so one refusal names every failing row
	groups, problems := groupRows(e, a.Match, rows)
	if len(groups) == 0 && problems != nil {
		return nil, problems
	}
	matched := map[string]string{}
	if a.Match != "" {
		found, ps := findMatches(d, deps, e, a.Match, groups)
		if ps != nil {
			return nil, ps
		}
		matched = found
	}
	// Each row's decision reads what the rows before it wrote (an autoNumber
	// counter), as a command graph's record nodes do
	var events []live.NewEvent
	sub := d
	if read := d.Read; read != nil && deps.Bus != nil {
		over, folded := deps.Bus.NewOverlay(), 0
		sub.Read = func(projection string, t live.Target) (json.RawMessage, error) {
			for ; folded < len(events); folded++ {
				ne := events[folded]
				ev := live.Event{Type: ne.Type, Target: *ne.Target, UserID: d.Principal.UserID, TenantID: d.Principal.TenantID, Time: time.Now().UTC()}
				ev.Data, _ = json.Marshal(ne.Data)
				if err := over.Apply(ev, read); err != nil {
					return nil, err
				}
			}
			if v, ok := over.Get(projection, t.Key()); ok {
				return v, nil
			}
			return read(projection, t)
		}
	}
	for _, g := range groups {
		var (
			out []live.NewEvent
			ps  []live.Problem
		)
		if id, ok := matched[g.key]; ok && g.key != "" {
			args, _ := json.Marshal(map[string]any{"entity": a.Entity, "set": g.values})
			out, _, ps = Write(sub, deps, "record.update", id, args)
		} else {
			args, _ := json.Marshal(map[string]any{"entity": a.Entity, "values": g.values})
			out, _, ps = Write(sub, deps, "record.create", "", args)
		}
		for _, p := range ps {
			p.Path = rowPath(g.row, p.Path)
			problems = append(problems, p)
		}
		if ps == nil {
			events = append(events, out...)
		}
	}
	if problems != nil {
		return nil, problems
	}
	return events, nil
}

// groupRows merges the rows by their match value, in order of first
// appearance; without match each row is its own group.
func groupRows(e records.EntityType, match string, rows []map[string]json.RawMessage) ([]upsertGroup, []live.Problem) {
	var groups []upsertGroup
	if match == "" {
		for i, r := range rows {
			groups = append(groups, upsertGroup{row: i, values: r})
		}
		return groups, nil
	}
	f, _, ok := e.FieldByID(match)
	if !ok || f.Hidden {
		return nil, problem("args.match", fmt.Sprintf("%s has no field %q", e.Name, match), "describe it with entity.describe {id: \""+e.ID+"\", published: true} for its fields' ids")
	}
	if records.IsComputed(f) {
		return nil, problem("args.match", f.Name+" is computed, so no row sets it", "match on a field the rows give")
	}
	if _, many := records.ItemOf(f); many {
		return nil, problem("args.match", f.Name+" holds a list, so it cannot name one record", "match on a field holding one value, such as an email")
	}
	at := map[string]int{}
	var problems []live.Problem
	for i, r := range rows {
		v, err := records.CheckValue(f, r[match])
		if err != nil {
			problems = append(problems, live.Problem{Path: rowPath(i, "args.values."+match), Message: f.Name + ": " + err.Error()})
			continue
		}
		if records.IsNull(v) {
			problems = append(problems, live.Problem{Path: rowPath(i, "args.values."+match), Message: f.Name + " is needed to match the row on", Fix: "give each row a " + f.Name})
			continue
		}
		key := string(v)
		if j, seen := at[key]; seen {
			for k, x := range r {
				groups[j].values[k] = x
			}
			continue
		}
		values := make(map[string]json.RawMessage, len(r))
		for k, x := range r {
			values[k] = x
		}
		at[key] = len(groups)
		groups = append(groups, upsertGroup{row: i, key: key, values: values})
	}
	return groups, problems
}

// findMatches is the record holding each group's match value, found
// through the match field's index when there is one (the read rule
// applies, so a hidden record is not matched).
func findMatches(d live.Decision, deps Deps, e records.EntityType, match string, groups []upsertGroup) (map[string]string, []live.Problem) {
	var keys []json.RawMessage
	for _, g := range groups {
		keys = append(keys, json.RawMessage(g.key))
	}
	values, _ := json.Marshal(keys)
	res, err := Find(d.ServiceArgs, deps.For(d.ServiceArgs), Query{Entity: e.ID, Filter: &Filter{Field: match, Op: "in", Value: values}, Fields: []string{match}})
	if err != nil {
		return nil, problem("", "the records to match cannot be read: "+err.Error(), "")
	}
	out := map[string]string{}
	for _, r := range res.Records {
		f, _, _ := e.FieldByID(match)
		v, err := records.CheckValue(f, r[match])
		if err != nil || records.IsNull(v) {
			continue
		}
		var id string
		_ = json.Unmarshal(r["_id"], &id)
		// The oldest record wins when several hold the value
		if prev, ok := out[string(v)]; !ok || compareIDs(id, prev) < 0 {
			out[string(v)] = id
		}
	}
	return out, nil
}

// rowPath names a row's problem: args.values.<field> or args.set.<field>
// of the row's own command becomes args.rows.<n>.<field>.
func rowPath(row int, path string) string {
	prefix := fmt.Sprintf("args.rows.%d", row)
	for _, p := range []string{"args.values.", "args.set."} {
		if rest, ok := strings.CutPrefix(path, p); ok {
			return prefix + "." + rest
		}
	}
	if path == "" || path == "target" || path == "args" {
		return prefix
	}
	return prefix + "." + strings.TrimPrefix(path, "args.")
}
