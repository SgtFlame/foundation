package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// Search finds records by the text of their searchable fields (Records.md
// › Built in slice 24): records/search keeps a LanceDB table per tenant,
// with an FTS index and, while a key lets it embed, vectors, and fuses the
// keyword and vector rankings as retrieval does. Search here reads the
// records it finds and keeps those the principal may read.

// Searcher is the records' search index (records/search).
type Searcher interface {
	// Follow starts keeping the principal's tenant's index.
	Follow(args features.ServiceArgs) error
	// Search ranks the tenant's records for a text, best first.
	Search(args features.ServiceArgs, q SearchQuery) ([]SearchHit, error)
	// Changes sends the index's position each time it changes in tenant,
	// until ctx ends.
	Changes(ctx context.Context, tenant uint64) <-chan live.Position
}

// SearchQuery is a search: text, in one entity type or all, with the
// lexical ↔ semantic dial retrieval has (-5 keywords only … +5 vectors
// mostly; 0 when left out).
type SearchQuery struct {
	Entity          string `json:"entity,omitempty"` // every entity type the principal may read when empty
	Text            string `json:"text"`
	Limit           int    `json:"limit,omitempty"` // 20 when left out, at most 100
	LexicalSemantic int    `json:"lexicalSemantic,omitempty"`
}

// SearchHit is one record the index found.
type SearchHit struct {
	Entity string
	ID     string
	Score  float64
}

// SearchResult is a search's records, best first, each with its _score.
type SearchResult struct {
	Records []Record `json:"records"`
}

// ListSearch is the list a records.search result's changes apply to.
const ListSearch = "records"

// SearchRecords runs a search once: the records found that exist and the
// principal may read, best first.
func SearchRecords(args features.ServiceArgs, d Deps, q SearchQuery) (SearchResult, error) {
	out := SearchResult{Records: []Record{}}
	if d.Search == nil {
		return out, log.Code("search_unavailable").Errorf("records are not searched here")
	}
	if strings.TrimSpace(q.Text) == "" {
		return out, nil
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	q.Limit = min(q.Limit, 100)
	if err := Follow(args, d); err != nil {
		return out, err
	}
	// Ask for more than the page: some may be hidden from the principal
	ask := q
	ask.Limit = q.Limit * 3
	hits, err := d.Search.Search(args, ask)
	if err != nil {
		return out, err
	}
	out.Records = Visible(args, d, hits, q.Limit)
	return out, nil
}

// Visible are the records hits name that exist and the principal may
// read, at most limit, each with its _score.
func Visible(args features.ServiceArgs, d Deps, hits []SearchHit, limit int) []Record {
	out := []Record{}
	types := map[string]*records.EntityType{}
	for _, h := range hits {
		e, seen := types[h.Entity]
		if !seen {
			types[h.Entity] = nil
			resource := records.Resource(args.Principal, h.Entity)
			if authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource) != nil {
				continue
			}
			t, problems := Entity(args, d.Projections, h.Entity)
			if problems != nil {
				continue
			}
			e, types[h.Entity] = &t, &t
		}
		if e == nil {
			continue
		}
		v, ok, err := d.Projections.Get(args.Context(), args.Principal.TenantID, Projector, Key(h.Entity, h.ID))
		var r Record
		if err != nil || !ok || v.Value == nil || json.Unmarshal(v.Value, &r) != nil || !records.Allows(e.Rule("read"), r, args.Principal) {
			continue
		}
		r["_score"] = raw(h.Score)
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out
}

// contributeSearch registers records.search.
func contributeSearch(reg *extensions.Registry, deps func() Deps) {
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "records.search", Source: "records"}, Title: "Search records",
		Description: "Records whose searchable fields match a text, best first, that stays current: {records}, each record with its _score. entity limits it to one entity type (every type you may read when left out); limit is how many (20, at most 100); lexicalSemantic (-5 … 5) leans to exact words or to meaning, as RAG Search's dial does. Only fields marked searchable are searched. It runs again, debounced, as the search index changes, sending insert, update, move and remove on the records list, keyed <entity>.<id>. Records a row rule hides are never shown.",
		Params:      json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string","description":"An entity type's id; every type when left out"},"text":{"type":"string"},"limit":{"type":"integer"},"lexicalSemantic":{"type":"integer","minimum":-5,"maximum":5}},"required":["text"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"entity":"page","text":"quarterly plan"}`)},
		Action:      "record.read", Resource: func(p features.Principal, params json.RawMessage) string {
			var q struct{ Entity string }
			_ = json.Unmarshal(params, &q)
			if q.Entity == "" {
				return authz.Tenant(p)
			}
			return records.Resource(p, q.Entity)
		},
		Run: republished(deps, func(features.ServiceArgs, Deps, json.RawMessage) []string { return nil }, func(f live.Feed) error {
			var q SearchQuery
			_ = json.Unmarshal(f.Params(), &q)
			d := deps()
			if d.Search == nil {
				return log.Code("search_unavailable").Errorf("records are not searched here")
			}
			args := f.Args()
			if err := Follow(args, d); err != nil {
				return err
			}
			return rerunOn(f, d, d.Search.Changes(f.Context(), args.Principal.TenantID), ListSearch, func() (computed, error) {
				res, err := SearchRecords(args, d, q)
				if err != nil {
					return computed{}, err
				}
				c := computed{result: res}
				for _, r := range res.Records {
					var id, entity string
					_ = json.Unmarshal(r[MetaID], &id)
					_ = json.Unmarshal(r[MetaEntity], &entity)
					c.items = append(c.items, item{key: fmt.Sprintf("%s.%s", entity, id), value: raw(r)})
				}
				return c, nil
			})
		}),
	})
}
