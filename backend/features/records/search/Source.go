package search

import (
	"context"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// SourceID is the records' retrieval source.
const SourceID = "records"

// Contribute registers the records as a retrieval source (Forge
// 03-DataModel question 10): optional, so chat searches them only when RAG
// Search's Records switch is on. A segment is a record the principal may
// read, its text its searchable fields'.
func Contribute(reg *extensions.Registry, index func() *Index) {
	retrieval.Sources.Register(reg, retrieval.Source{
		Descriptor: extensions.Descriptor{ID: SourceID, Order: 300, Source: "records"},
		Title:      "Records", Axis: retrieval.AxisLibrary, Optional: true,
		VectorSearch: func(ctx context.Context, vector []float32, k int, _ retrieval.Filter) ([]retrieval.Segment, error) {
			x := index()
			args, ok := callArgs(ctx)
			if x == nil || !ok {
				return nil, nil
			}
			s, err := x.For(ctx, args.Principal.TenantID)
			if err != nil {
				return nil, err
			}
			found, err := s.VectorSearch(ctx, "", vector, k)
			if err != nil {
				return nil, err
			}
			return x.segments(args, found), nil
		},
		KeywordSearch: func(ctx context.Context, query string, k int, _ retrieval.Filter) ([]retrieval.Segment, error) {
			x := index()
			args, ok := callArgs(ctx)
			if x == nil || !ok {
				return nil, nil
			}
			s, err := x.For(ctx, args.Principal.TenantID)
			if err != nil {
				return nil, err
			}
			found, err := s.KeywordSearch(ctx, "", query, k)
			if err != nil {
				return nil, err
			}
			return x.segments(args, found), nil
		},
	})
}

// callArgs are the searching principal's args; ok is false for a call
// without one, which finds nothing.
func callArgs(ctx context.Context) (features.ServiceArgs, bool) {
	p, meta, ok := features.CallFrom(ctx)
	return features.ServiceArgs{Ctx: ctx, Principal: p, Meta: meta}, ok
}

// segments are the matches the principal may read, as segments.
func (x *Index) segments(args features.ServiceArgs, found []Match) []retrieval.Segment {
	hits := make([]data.SearchHit, 0, len(found))
	byKey := map[string]Match{}
	for _, m := range found {
		hits = append(hits, data.SearchHit{Entity: m.Entity, ID: m.RecordID})
		byKey[data.Key(m.Entity, m.RecordID)] = m
	}
	var out []retrieval.Segment
	for _, r := range data.Visible(args, x.deps(), hits, len(hits)) {
		var id, entity string
		_ = json.Unmarshal(r[data.MetaID], &id)
		_ = json.Unmarshal(r[data.MetaEntity], &entity)
		m := byKey[data.Key(entity, id)]
		title := m.Title
		if title == "" {
			title = entity + " " + id
		}
		out = append(out, retrieval.Segment{
			SourceID: SourceID, ID: data.Key(entity, id), Key: data.Key(entity, id) + "@" + string(r[data.MetaSeq]),
			Title: entity + " › " + title, Text: m.Text, Location: retrieval.Location{Entity: entity, RecordID: id},
		})
	}
	return out
}
