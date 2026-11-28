package recordsnodes

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// PrepareRowsID is Prepare rows' node type id (forge 40c).
const PrepareRowsID = "records.prepareRows"

// contributePrepare registers Prepare rows: an import's rows checked and
// normalized before record.upsert sends them (the CSV import wizard's
// preview, forge 40c).
func contributePrepare(reg *extensions.Registry, deps func() data.Deps) {
	node(reg, 694, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: PrepareRowsID}, Title: "Prepare rows",
		Description: "Checks rows of values by field id against an entity type's published fields before an import, as record.create checks them, without writing: each value is normalized for its field (numbers, decimals, dates, enums by option id or name), unknown and computed fields are problems, and a required field without a value is one for a row that will be created. " +
			"With resolve, a relation field's values may be the related records' titles (a contact's Company column holding company names): those found are replaced by their ids, and those not found are listed in missing, by field, for the graph to create (record.upsert matching the target's title field) before preparing again. With match, rows whose match value a record already holds are counted in matched, as record.upsert would update them. The read rule applies to every lookup.",
		Example:  "Prepare rows of the mapped CSV rows for contact with resolve on and match email, then show the problems in the preview, and send the OK rows with record.upsert in batches of 500",
		Examples: cognition.ConfigExample(PrepareRowsID, `{"entity":"contact","resolve":true}`),
		Tags:     []string{"import", "csv", "validate", "check", "rows", "records", "upsert"},
		Kinds:    []string{livenodes.KindAction, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent, livenodes.KindTrigger},
		Config: json.RawMessage(`{"type":"object","properties":{` + entityConfig + `,` +
			`"resolve":{"type":"boolean","title":"Resolve titles","description":"Relation values that are not ids are looked up by the related type's title field"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.DataPin("rows", "Rows", "json", "The rows: [{field id: value}]"),
				{ID: "entity", Name: "Entity type", Type: "string", Optional: true, Description: "The entity type's id, in place of the config's"},
				{ID: "match", Name: "Match", Type: "string", Optional: true, Description: "The field record.upsert will match on, to count the rows it updates; none when unconnected"},
			},
			Out: []cognition.Pin{
				cognition.DataPin("rows", "Rows", "json", "Every row normalized, the fields with problems left out"),
				cognition.DataPin("ok", "OK rows", "json", "The normalized rows without problems, for record.upsert"),
				cognition.DataPin("okIndex", "OK indexes", "json", "Each OK row's index in the rows given"),
				cognition.DataPin("problems", "Problems", "json", "[{row, field, message}], row from 0"),
				cognition.DataPin("failed", "Failed", "json", "The indexes of the rows with problems, in order"),
				cognition.DataPin("missing", "Missing", "json", "Relation titles not found: [{field, entity, titleField, titles}]"),
				cognition.DataPin("matched", "Matched", "number", "How many OK rows a record already matches (with match)"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				Entity  string
				Resolve bool
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			if e, _ := cognition.Get[string](in, "entity"); e != "" {
				c.Entity = e
			}
			match, _ := cognition.Get[string](in, "match")
			var rows []map[string]json.RawMessage
			if raw, err := json.Marshal(in["rows"]); err == nil && string(raw) != "null" {
				if err := json.Unmarshal(raw, &rows); err != nil {
					return nil, "", fmt.Errorf("rows must be a list of objects: %v", err)
				}
			}
			d := deps().For(x.ServiceArgs)
			out, err := prepareRows(x.ServiceArgs, d, c.Entity, rows, c.Resolve, match)
			if err != nil {
				return nil, "", err
			}
			return out, "", nil
		},
	})
}

type rowProblem struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type missingTitles struct {
	Field      string   `json:"field"`
	Entity     string   `json:"entity"`
	TitleField string   `json:"titleField"`
	Titles     []string `json:"titles"`
}

func prepareRows(args features.ServiceArgs, d data.Deps, entity string, rows []map[string]json.RawMessage, resolve bool, match string) (cognition.Values, error) {
	e, problems := data.Entity(args, d.Projections, entity)
	if problems != nil {
		return nil, fmt.Errorf("%s", problems[0].Message)
	}
	var probs []rowProblem
	missing := map[string]*missingTitles{}
	// Relation titles to ids, per field, when resolving
	resolved := map[string]map[string]string{}
	if resolve {
		for _, f := range e.Visible() {
			if f.Type != "relation" || f.Relation == nil || f.Relation.Entity == "user" {
				continue
			}
			ids, miss, err := resolveTitles(args, d, f, rows)
			if err != nil {
				return nil, err
			}
			resolved[f.ID] = ids
			if len(miss) > 0 {
				target, _ := data.Entity(args, d.Projections, f.Relation.Entity)
				missing[f.ID] = &missingTitles{Field: f.ID, Entity: f.Relation.Entity, TitleField: target.TitleField, Titles: miss}
			}
		}
	}
	norm := make([]map[string]any, len(rows))
	bad := make([]bool, len(rows))
	for i, r := range rows {
		norm[i] = map[string]any{}
		for id, v := range r {
			f, _, ok := e.FieldByID(id)
			if !ok || f.Hidden {
				probs = append(probs, rowProblem{i, id, fmt.Sprintf("%s has no field %q", e.Name, id)})
				bad[i] = true
				continue
			}
			if records.IsComputed(f) {
				probs = append(probs, rowProblem{i, id, f.Name + " is computed, so an import cannot set it"})
				bad[i] = true
				continue
			}
			if ids, ok := resolved[id]; ok {
				v = relationValue(f, v, ids)
			}
			out, err := records.CheckValue(f, v)
			if err != nil {
				probs = append(probs, rowProblem{i, id, f.Name + ": " + err.Error()})
				bad[i] = true
				continue
			}
			if out != nil {
				norm[i][id] = json.RawMessage(out)
			}
		}
	}
	// Which rows a record already matches, so required fields are only
	// needed by the rows to be created
	existing := map[string]bool{}
	if match != "" {
		f, _, ok := e.FieldByID(match)
		if !ok {
			return nil, fmt.Errorf("%s has no field %q to match on", e.Name, match)
		}
		var keys []json.RawMessage
		for _, r := range norm {
			if v, ok := r[match].(json.RawMessage); ok {
				keys = append(keys, v)
			}
		}
		if len(keys) > 0 {
			values, _ := json.Marshal(keys)
			res, err := data.Find(args, d, data.Query{Entity: e.ID, Filter: &data.Filter{Field: match, Op: "in", Value: values}, Fields: []string{match}})
			if err != nil {
				return nil, err
			}
			for _, rec := range res.Records {
				if v, err := records.CheckValue(f, rec[match]); err == nil && v != nil {
					existing[string(v)] = true
				}
			}
		}
	}
	matched := 0
	for i, r := range norm {
		key, hasKey := r[match].(json.RawMessage)
		updates := match != "" && hasKey && existing[string(key)]
		if updates && !bad[i] {
			matched++
		}
		if updates {
			continue
		}
		for _, f := range e.Visible() {
			if !f.Required || records.IsComputed(f) {
				continue
			}
			if _, set := r[f.ID]; set {
				continue
			}
			if !records.IsNull(f.Default) {
				continue
			}
			if !problemFor(probs, i, f.ID) { // a value given but refused is a problem already
				probs = append(probs, rowProblem{i, f.ID, f.Name + " is required"})
				bad[i] = true
			}
		}
	}
	sort.SliceStable(probs, func(a, b int) bool { return probs[a].Row < probs[b].Row })
	ok, okIndex, failed := []any{}, []any{}, []any{}
	for i := range norm {
		if bad[i] {
			failed = append(failed, i)
			continue
		}
		ok = append(ok, norm[i])
		okIndex = append(okIndex, i)
	}
	var miss []any
	for _, f := range e.Visible() {
		if m, ok := missing[f.ID]; ok {
			miss = append(miss, m)
		}
	}
	if miss == nil {
		miss = []any{}
	}
	all := make([]any, len(norm))
	for i := range norm {
		all[i] = norm[i]
	}
	pv := plain(probs)
	if pv == nil {
		pv = []any{}
	}
	return cognition.Values{"rows": plain(all), "ok": plain(ok), "okIndex": okIndex, "problems": pv, "failed": failed, "missing": plain(miss), "matched": matched}, nil
}

func problemFor(probs []rowProblem, row int, field string) bool {
	for _, p := range probs {
		if p.Row == row && p.Field == field {
			return true
		}
	}
	return false
}

// relationTexts are a relation cell's texts: a string (comma-separated
// for a to-many field), or a list of them.
func relationTexts(f records.Field, v json.RawMessage) []string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		if f.Relation != nil && f.Relation.Many {
			var out []string
			for _, p := range strings.Split(s, ",") {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			return out
		}
		if s = strings.TrimSpace(s); s != "" {
			return []string{s}
		}
		return nil
	}
	var list []string
	if json.Unmarshal(v, &list) == nil {
		return list
	}
	return nil
}

// relationValue is a relation cell with its titles replaced by ids;
// titles not found are left, for CheckValue to refuse.
func relationValue(f records.Field, v json.RawMessage, ids map[string]string) json.RawMessage {
	texts := relationTexts(f, v)
	if texts == nil {
		return v
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		if id, ok := ids[strings.ToLower(t)]; ok {
			out[i] = id
		} else {
			out[i] = t
		}
	}
	if f.Relation.Many {
		b, _ := json.Marshal(out)
		return b
	}
	b, _ := json.Marshal(out[0])
	return b
}

// resolveTitles maps a relation field's cell texts (lowercased) to record
// ids: an existing record's id stays itself, else a record whose title
// field holds the text (the oldest when several do); texts not found are
// returned in order of first appearance.
func resolveTitles(args features.ServiceArgs, d data.Deps, f records.Field, rows []map[string]json.RawMessage) (map[string]string, []string, error) {
	var texts []string
	seen := map[string]bool{}
	for _, r := range rows {
		v, ok := r[f.ID]
		if !ok {
			continue
		}
		for _, t := range relationTexts(f, v) {
			if !seen[strings.ToLower(t)] {
				seen[strings.ToLower(t)] = true
				texts = append(texts, t)
			}
		}
	}
	ids := map[string]string{}
	if len(texts) == 0 {
		return ids, nil, nil
	}
	target, problems := data.Entity(args, d.Projections, f.Relation.Entity)
	if problems != nil {
		return nil, nil, fmt.Errorf("%s", problems[0].Message)
	}
	values, _ := json.Marshal(texts)
	byID, err := data.Find(args, d, data.Query{Entity: target.ID, Filter: &data.Filter{Field: "_id", Op: "in", Value: values}, Fields: []string{"_id"}})
	if err != nil {
		return nil, nil, err
	}
	for _, r := range byID.Records {
		var id string
		_ = json.Unmarshal(r["_id"], &id)
		ids[strings.ToLower(id)] = id
	}
	if target.TitleField != "" {
		var rest []string
		for _, t := range texts {
			if _, ok := ids[strings.ToLower(t)]; !ok {
				rest = append(rest, t)
			}
		}
		if len(rest) > 0 {
			// Titles are matched ignoring case: every record's title is read
			// once (an import's lookups are few), through the title's index
			// when there is one
			res, err := data.Find(args, d, data.Query{Entity: target.ID, Sort: []data.Sort{{Field: target.TitleField}}, Fields: []string{target.TitleField}})
			if err != nil {
				return nil, nil, err
			}
			want := map[string]bool{}
			for _, t := range rest {
				want[strings.ToLower(strings.TrimSpace(t))] = true
			}
			for _, r := range res.Records {
				var title, id string
				_ = json.Unmarshal(r[target.TitleField], &title)
				_ = json.Unmarshal(r["_id"], &id)
				k := strings.ToLower(strings.TrimSpace(title))
				if want[k] {
					if prev, ok := ids[k]; !ok || len(id) < len(prev) || (len(id) == len(prev) && id < prev) {
						ids[k] = id
					}
				}
			}
		}
	}
	var miss []string
	for _, t := range texts {
		if _, ok := ids[strings.ToLower(t)]; !ok {
			miss = append(miss, t)
		}
	}
	return ids, miss, nil
}
