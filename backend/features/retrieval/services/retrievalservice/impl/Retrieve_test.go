package impl

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval/settings"
)

type sections map[string]string

func (s sections) Resolved(_ features.ServiceArgs, section string) (json.RawMessage, error) {
	if v, ok := s[section]; ok {
		return json.RawMessage(v), nil
	}
	return nil, context.Canceled
}

// fakeEmbedder embeds every query as [1], or fails like a provider without a key.
type fakeEmbedder struct {
	embedservice.Service
	fail bool
}

func (f fakeEmbedder) Embed(args embedservice.EmbedArgs) embedservice.EmbedResponse {
	if f.fail {
		return embedservice.EmbedResponse{Common: features.ServiceResponse{Code: http.StatusServiceUnavailable}}
	}
	return embedservice.EmbedResponse{Common: features.OK(), Vectors: [][]float32{{1}}}
}

// seg is a library chunk of file at [start, end) whose text is filler of
// that length.
func seg(source, file string, start, end int, key string) retrieval.Segment {
	return retrieval.Segment{
		SourceID: source, ID: file + "#" + key, Key: key, Title: file, Text: strings.Repeat("x", end-start),
		Location: retrieval.Location{Path: file, Start: start, End: end, StartLine: start / 10, EndLine: end / 10},
	}
}

func newTestService(t *testing.T, embedder embedservice.Service, sources ...retrieval.Source) *service {
	t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, retrieval.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	for _, src := range sources {
		retrieval.Sources.Register(reg, src)
	}
	config := sections{
		retrievalsettings.ID:         `{"rrfK": 60, "candidateFactor": 5, "minCandidates": 50, "axisBase": 4, "defaultTopK": 8, "budgetShare": 0.5, "maxBudgetTokens": 10000}`,
		retrievalsettings.FeedbackID: `{"gamma": 0.5, "min": 0.25, "max": 4}`,
	}
	return newService(WithDB(gdb), WithEmbedder(embedder), WithConfig(config), WithRegistry(reg))
}

func source(id string, axis retrieval.Axis, vector, keyword []retrieval.Segment) retrieval.Source {
	return retrieval.Source{
		Descriptor: extensions.Descriptor{ID: id}, Axis: axis,
		VectorSearch: func(context.Context, []float32, int, retrieval.Filter) ([]retrieval.Segment, error) {
			return vector, nil
		},
		KeywordSearch: func(context.Context, string, int, retrieval.Filter) ([]retrieval.Segment, error) {
			return keyword, nil
		},
	}
}

var local = features.ServiceArgs{Principal: features.LocalPrincipal()}

func TestRetrieveFusesWeighsAndGrounds(t *testing.T) {
	a, b := seg("library", "a.md", 0, 100, "ha"), seg("library", "b.md", 0, 100, "hb")
	m := seg("chronicle", "", 0, 0, "m1")
	m.Text, m.Title = "a memory", "Memory"
	s := newTestService(t, fakeEmbedder{},
		source("library", retrieval.AxisLibrary, []retrieval.Segment{a, b}, []retrieval.Segment{b}),
		source("chronicle", retrieval.AxisChronicle, []retrieval.Segment{m}, nil))

	resp := s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q"})
	if resp.Common.Code != http.StatusOK || len(resp.Segments) != 3 || resp.Candidates != 3 {
		t.Fatalf("resp %+v", resp)
	}
	// b is found by both searches, so it beats a, first in one
	if resp.Segments[0].Key != "hb" || resp.Segments[0].Number != 1 || resp.Segments[0].VectorRank != 2 || resp.Segments[0].KeywordRank != 1 {
		t.Errorf("first %+v", resp.Segments[0])
	}
	if want := 0.5/62 + 0.5/61; math.Abs(resp.Segments[0].RRF-want) > 1e-12 || resp.Segments[0].AxisMult != 1 || resp.Segments[0].Grounding != 1 {
		t.Errorf("first's numbers %+v", resp.Segments[0])
	}

	// Axis 2 at +5 favours the library 4:1 and disfavours Chronicle 1:4
	resp = s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q", ChronicleLibrary: 5})
	for _, sg := range resp.Segments {
		want := 4.0
		if sg.SourceID == "chronicle" {
			want = 0.25
		}
		if math.Abs(sg.AxisMult-want) > 1e-9 {
			t.Errorf("%s axis multiplier %v, want %v", sg.SourceID, sg.AxisMult, want)
		}
	}

	// 👎 on a round that used a lowers it; then 👍 moves the vote
	rs := []retrieval.RoundSegment{{SourceID: "library", SegmentKey: "ha"}}
	if err := retrieval.Vote(s.db, 1, rs, retrieval.FeedbackNone, retrieval.FeedbackIncorrect); err != nil {
		t.Fatal(err)
	}
	resp = s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q"})
	if g := find(resp.Segments, "ha").Grounding; math.Abs(g-math.Sqrt(0.5)) > 1e-9 {
		t.Errorf("grounding after 👎 %v", g)
	}
	if err := retrieval.Vote(s.db, 1, rs, retrieval.FeedbackIncorrect, retrieval.FeedbackCorrect); err != nil {
		t.Fatal(err)
	}
	var f retrieval.SegmentFeedback
	s.db.First(&f)
	if f.Up != 1 || f.Down != 0 {
		t.Errorf("moved vote %+v", f)
	}
	// Another tenant's feedback does not count
	other := local
	other.Principal.TenantID = 2
	if g := find(s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: other, Query: "q"}).Segments, "ha").Grounding; g != 1 {
		t.Errorf("other tenant's grounding %v", g)
	}

	// Only the sources asked for
	resp = s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q", Sources: []string{"chronicle"}})
	if len(resp.Segments) != 1 || resp.Segments[0].SourceID != "chronicle" {
		t.Errorf("chronicle only %+v", resp.Segments)
	}
	if code := s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q", LexicalSemantic: 6}).Common.Code; code != http.StatusBadRequest {
		t.Errorf("axis 6: code %d", code)
	}
}

func TestRetrieveFallsBackToKeywords(t *testing.T) {
	a, b := seg("library", "a.md", 0, 100, "ha"), seg("library", "b.md", 0, 100, "hb")
	s := newTestService(t, fakeEmbedder{fail: true}, source("library", retrieval.AxisLibrary, []retrieval.Segment{a}, []retrieval.Segment{b}))
	resp := s.Retrieve(retrievalservice.RetrieveArgs{ServiceArgs: local, Query: "q"})
	if !resp.VectorSkipped || len(resp.Segments) != 1 || resp.Segments[0].Key != "hb" || resp.Segments[0].RRF != 1.0/61 {
		t.Errorf("keyword fallback %+v", resp)
	}
}

func TestAssembleMergesDedupesAndBudgets(t *testing.T) {
	scored := func(s retrieval.Segment, score float64) retrieval.Scored {
		return retrieval.Scored{Segment: s, Keys: []string{s.Key}, Score: score, Tokens: tokens(s.Text)}
	}
	first := seg("library", "a.md", 100, 200, "k1")
	overlap := seg("library", "a.md", 150, 260, "k2")
	touching := seg("library", "a.md", 40, 100, "k3")
	copyOf := seg("library", "c.md", 0, 100, "k1") // the same text as first, elsewhere
	big := seg("library", "d.md", 0, 4000, "k4")   // 1000 tokens
	small := seg("library", "e.md", 0, 40, "k5")
	got := assemble([]retrieval.Scored{
		scored(small, 0.1), scored(first, 0.9), scored(overlap, 0.8), scored(copyOf, 0.7), scored(touching, 0.6), scored(big, 0.5),
	}, 8, 200)
	if len(got) != 2 {
		t.Fatalf("got %d segments: %+v", len(got), got)
	}
	m := got[0]
	if m.Key != "k1" || m.Location.Start != 40 || m.Location.End != 260 || len(m.Text) != 220 || strings.Join(m.Keys, ",") != "k1,k2,k3" ||
		len(m.Also) != 1 || m.Also[0] != "c.md" || m.Number != 1 || m.Score != 0.9 {
		t.Errorf("merged %+v", m)
	}
	// big does not fit the budget, so the next one is tried
	if got[1].Key != "k5" || got[1].Number != 2 {
		t.Errorf("second %+v", got[1])
	}
	if n := len(assemble([]retrieval.Scored{scored(small, 1), scored(big, 0.5), scored(first, 0.2)}, 1, 10000)); n != 1 {
		t.Errorf("top-K 1 gave %d", n)
	}
}

func TestGrounding(t *testing.T) {
	g := retrieval.GroundingSettings{Gamma: 0.5, Min: 0.25, Max: 4}
	for _, c := range []struct {
		up, down int
		want     float64
	}{{0, 0, 1}, {1, 0, math.Sqrt2}, {0, 1, math.Sqrt(0.5)}, {100, 0, 4}, {0, 100, 0.25}} {
		if got := g.Grounding(c.up, c.down); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Grounding(%d, %d) = %v, want %v", c.up, c.down, got, c.want)
		}
	}
}

func find(segments []retrieval.Scored, key string) retrieval.Scored {
	for _, s := range segments {
		if s.Key == key {
			return s
		}
	}
	return retrieval.Scored{}
}
