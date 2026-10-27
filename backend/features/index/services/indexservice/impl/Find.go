package impl

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/index/services/indexservice"
)

// maxCount is the most documents one Find returns.
const maxCount = 100

// snippetRunes is the length of a chunk's snippet.
const snippetRunes = 240

// Find searches by vector similarity and BM25 keywords, fuses the two
// rankings by args.Weight and returns the best args.Count documents.
func (s *service) Find(args indexservice.FindArgs) indexservice.FindResponse {
	query := strings.TrimSpace(args.Query)
	switch {
	case s.store == nil:
		return findFail(args, http.StatusServiceUnavailable, errNotConfigured())
	case query == "":
		return findFail(args, http.StatusBadRequest, log.Code("empty_query").New("the query is empty"))
	case args.Weight < 0 || args.Weight > 1:
		return findFail(args, http.StatusBadRequest, log.Code("invalid_weight").New("weight must be between 0 and 1"))
	case args.Count < 0 || args.Count > maxCount:
		return findFail(args, http.StatusBadRequest, log.Code("invalid_count").New("count must be between 1 and 100"))
	}
	count := args.Count
	if count == 0 {
		count = 10
	}
	ctx := args.Context()
	k := max(5*count, 50) // candidate chunks from each search
	filter := indexservice.Filter{VolumeIDs: args.VolumeIDs}

	var vector, keyword []indexservice.Match
	if args.Weight > 0 {
		if s.embedder == nil {
			return findFail(args, http.StatusServiceUnavailable, errNotConfigured())
		}
		embedArgs := args.ServiceArgs
		embedArgs.Meta = embedArgs.Meta.WithFeature("embed-query")
		resp := s.embedder.Embed(embedservice.EmbedArgs{ServiceArgs: embedArgs, Texts: []string{query}, Purpose: embedservice.PurposeQuery})
		if f := failed(resp.Common); f != nil {
			return indexservice.FindResponse{Common: *f}
		}
		var err error
		if vector, err = s.store.VectorSearch(ctx, resp.Vectors[0], k, filter); err != nil {
			return findFail(args, http.StatusInternalServerError, log.Wrap(err))
		}
	}
	if args.Weight < 1 {
		var err error
		if keyword, err = s.store.KeywordSearch(ctx, query, k, filter); err != nil {
			return findFail(args, http.StatusInternalServerError, log.Wrap(err))
		}
	}

	terms := queryTerms(query)
	var hits []indexservice.Hit
	for _, d := range group(fuseMatches(vector, keyword, args.Weight), count) {
		meta, _ := indexservice.ReadMeta(s.abs(d.path)) // positions are best effort
		dirs, fileType := folders(d.path)
		hit := indexservice.Hit{Folders: dirs, Type: fileType, Score: d.score}
		for _, c := range d.chunks {
			hit.VolumeID = c.Item.VolumeID
			ref := indexservice.ChunkRef{
				Snippet:     snippet(c.Item.Text, terms),
				Score:       c.Score,
				VectorRank:  c.VectorRank,
				KeywordRank: c.KeywordRank,
			}
			if c.Item.Ordinal < len(meta.Chunks) {
				m := meta.Chunks[c.Item.Ordinal]
				ref.Start, ref.End, ref.StartLine, ref.EndLine = m.Start, m.End, m.StartLine, m.EndLine
				ref.Heading, ref.Head, ref.Tail = m.Heading, m.Head, m.Tail
			}
			hit.Chunks = append(hit.Chunks, ref)
		}
		hits = append(hits, hit)
	}
	return indexservice.FindResponse{Common: features.OK(), Hits: hits}
}

func findFail(args indexservice.FindArgs, code uint16, err error) indexservice.FindResponse {
	return indexservice.FindResponse{Common: *args.Fail(code, err)}
}

func errNotConfigured() error {
	return log.Code("index_not_configured").Public("the index is not configured").New("the index is not configured")
}

// queryTerms are the query's words, lower-cased.
func queryTerms(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// snippet is about snippetRunes of text, starting a little before the first
// query term it contains, or at the start.
func snippet(text string, terms []string) string {
	text = strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(text)
	start := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (start < 0 || i < start) {
			start = i
		}
	}
	runes := []rune(text)
	from := 0
	if start > len(text) { // lower-casing changed the byte length
		start = 0
	}
	if start > 0 {
		from = max(len([]rune(text[:start]))-snippetRunes/4, 0)
	}
	to := min(from+snippetRunes, len(runes))
	out := string(runes[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}
