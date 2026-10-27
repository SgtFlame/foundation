package conversationdto

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// RoundSegment is one segment a round injected, cited as [number], with the
// numbers the Context inspector shows: score = rrf · axisMult · grounding.
type RoundSegment struct {
	Number      int                `json:"number"`
	SourceID    string             `json:"sourceId"`
	Key         string             `json:"key"`
	Title       string             `json:"title"`
	Snippet     string             `json:"snippet"`
	Location    retrieval.Location `json:"location"`
	Also        []string           `json:"also,omitempty"`
	Score       float64            `json:"score"`
	RRF         float64            `json:"rrf"`
	AxisMult    float64            `json:"axisMult"`
	Grounding   float64            `json:"grounding"`
	VectorRank  int                `json:"vectorRank,omitempty"`
	KeywordRank int                `json:"keywordRank,omitempty"`
	Pinned      bool               `json:"pinned,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	Tokens      int                `json:"tokens"`
}

func RoundSegmentFrom(s retrieval.RoundSegment) RoundSegment {
	out := RoundSegment{
		Number: s.Number, SourceID: s.SourceID, Key: s.SegmentKey, Title: s.Title, Snippet: s.Snippet,
		Score: s.Score, RRF: s.RRF, AxisMult: s.AxisMult, Grounding: s.Grounding,
		VectorRank: s.VectorRank, KeywordRank: s.KeywordRank, Pinned: s.Pinned, Reason: s.Reason, Tokens: s.Tokens,
	}
	_ = json.Unmarshal([]byte(s.Location), &out.Location)
	if s.Also != "" {
		out.Also = strings.Split(s.Also, "\n")
	}
	return out
}
