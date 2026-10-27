package impl

import (
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/fuse"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval/settings"
)

const maxTopK = 50

// Retrieve runs the pipeline: one query embedding, both searches of every
// enabled source concurrently, per-source RRF, the axis and grounding
// multipliers, then deduplication and the window.
func (s *service) Retrieve(args retrievalservice.RetrieveArgs) retrievalservice.RetrieveResponse {
	const action = "retrieval.read"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return fail(args, authz.Status, err)
	}
	query := strings.TrimSpace(args.Query)
	switch {
	case query == "":
		return fail(args, http.StatusBadRequest, log.Code("empty_query").New("the query is empty"))
	case args.LexicalSemantic < -5 || args.LexicalSemantic > 5 || args.ChronicleLibrary < -5 || args.ChronicleLibrary > 5:
		return fail(args, http.StatusBadRequest, log.Code("invalid_axis").New("the dials go from -5 to 5"))
	case args.TopK < 0 || args.TopK > maxTopK:
		return fail(args, http.StatusBadRequest, log.Code("invalid_top_k").Errorf("top-K must be between 1 and %d", maxTopK))
	case s.config == nil:
		return fail(args, http.StatusServiceUnavailable, log.Code("retrieval_not_configured").New("retrieval is not configured"))
	}
	conf, err := config.Read[retrievalsettings.Retrieval](s.config, args.ServiceArgs, retrievalsettings.ID)
	if err != nil {
		return fail(args, http.StatusInternalServerError, log.Wrap(err))
	}
	grounding, err := config.Read[retrievalsettings.Feedback](s.config, args.ServiceArgs, retrievalsettings.FeedbackID)
	if err != nil {
		return fail(args, http.StatusInternalServerError, log.Wrap(err))
	}
	topK := args.TopK
	if topK == 0 {
		topK = max(conf.DefaultTopK, 1)
	}
	budget := conf.MaxBudgetTokens
	if args.ContextWindow > 0 {
		budget = min(budget, int(conf.BudgetShare*float64(args.ContextWindow)))
	}
	resp := retrievalservice.RetrieveResponse{Common: features.OK(), BudgetTokens: budget}

	var sources []retrieval.Source
	for _, src := range retrieval.Sources.All(s.registry) {
		if len(args.Sources) == 0 || slices.Contains(args.Sources, src.ID) {
			sources = append(sources, src)
		}
	}
	if len(sources) == 0 {
		return resp
	}

	// Axis 1: the vector search's share of each ranking; at either end the
	// unused search is skipped
	weight := float64(args.LexicalSemantic+5) / 10
	var vector []float32
	if weight > 0 {
		if vector = s.embed(args, query); vector == nil {
			resp.VectorSkipped = true
			weight = 0 // the keyword ranking alone
		}
	}

	k := max(conf.CandidateFactor*topK, conf.MinCandidates, topK)
	filter := retrieval.Filter{VolumeIDs: args.VolumeIDs}
	ctx := args.CallContext()
	ranked := make([][]fuse.Ranked[retrieval.Segment], len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Go(func() {
			var vec, kw []retrieval.Segment
			var err error
			if vector != nil && src.VectorSearch != nil {
				if vec, err = src.VectorSearch(ctx, vector, k, filter); err != nil {
					log.With("source", src.ID).Warn(args.Context(), "the %s source's vector search failed, so it is skipped: %v", src.ID, err)
					return
				}
			}
			if weight < 1 && src.KeywordSearch != nil {
				if kw, err = src.KeywordSearch(ctx, query, k, filter); err != nil {
					log.With("source", src.ID).Warn(args.Context(), "the %s source's keyword search failed, so it is skipped: %v", src.ID, err)
					return
				}
			}
			ranked[i] = fuse.Fuse(vec, kw, func(seg retrieval.Segment) string { return seg.ID }, weight, conf.RRFK)
		})
	}
	wg.Wait()

	// Axis 2 and grounding
	var candidates []retrieval.Scored
	for i, src := range sources {
		mult := axisMultiplier(src.Axis, args.ChronicleLibrary, conf.AxisBase)
		counts, err := s.feedback(args, src.ID, ranked[i])
		if err != nil {
			log.With("source", src.ID).Warn(args.Context(), "cannot read the %s source's feedback, so it goes ungrounded: %v", src.ID, err)
		}
		for _, r := range ranked[i] {
			c := counts[r.Item.Key]
			g := grounding.Grounding(c[0], c[1])
			candidates = append(candidates, retrieval.Scored{
				Segment: r.Item, Keys: []string{r.Item.Key}, RRF: r.Score, AxisMult: mult, Grounding: g, Score: r.Score * mult * g,
				VectorRank: r.VectorRank, KeywordRank: r.KeywordRank, Tokens: tokens(r.Item.Text),
			})
		}
	}
	resp.Candidates = len(candidates)
	resp.Segments = assemble(candidates, topK, budget)
	return resp
}

// embed returns the query's vector, or nil when embedding fails: the
// failure is logged by Fail, and the round goes on with keywords.
func (s *service) embed(args retrievalservice.RetrieveArgs, query string) []float32 {
	if s.embedder == nil {
		return nil
	}
	call := args.ServiceArgs
	call.Meta = call.Meta.WithFeature("embed-query")
	resp := s.embedder.Embed(embedservice.EmbedArgs{ServiceArgs: call, Texts: []string{query}, Purpose: embedservice.PurposeQuery})
	if resp.Common.Code >= 300 || len(resp.Vectors) == 0 {
		return nil
	}
	return resp.Vectors[0]
}

// feedback reads the up and down counts of the ranked segments' keys.
func (s *service) feedback(args retrievalservice.RetrieveArgs, source string, ranked []fuse.Ranked[retrieval.Segment]) (map[string][2]int, error) {
	out := map[string][2]int{}
	if s.db == nil || len(ranked) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(ranked))
	for _, r := range ranked {
		if r.Item.Key != "" {
			keys = append(keys, r.Item.Key)
		}
	}
	var rows []retrieval.SegmentFeedback
	err := s.db.WithContext(args.Context()).Scopes(db.InTenant(args.Principal.TenantID)).
		Where("source_id = ? AND segment_key IN ?", source, keys).Find(&rows).Error
	for _, f := range rows {
		out[f.SegmentKey] = [2]int{f.Up, f.Down}
	}
	return out, err
}

// axisMultiplier is B^(a/5) for the library and B^(−a/5) for Chronicle
// memory: exponential, so each notch is an even step, and ±5 favours one
// side B² : 1 without excluding the other.
func axisMultiplier(axis retrieval.Axis, a int, base float64) float64 {
	if base <= 0 {
		base = 4
	}
	if axis == retrieval.AxisChronicle {
		a = -a
	}
	return math.Pow(base, float64(a)/5)
}

// tokens estimates a text's tokens at about four characters each.
func tokens(text string) int {
	return (len(text) + 3) / 4
}

func fail(args retrievalservice.RetrieveArgs, code uint16, err error) retrievalservice.RetrieveResponse {
	return retrievalservice.RetrieveResponse{Common: *args.Fail(code, err)}
}
