package retrievaldto

import "github.com/SgtFlame/foundation/backend/features/retrieval"

// Segment is a selected segment with the numbers that explain its choice:
// score = rrf · axisMult · grounding.
type Segment struct {
	Number      int                `json:"number"`
	SourceID    string             `json:"sourceId"`
	Key         string             `json:"key"`
	Title       string             `json:"title"`
	Text        string             `json:"text"`
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

func SegmentFrom(s retrieval.Scored) Segment {
	return Segment{
		Number: s.Number, SourceID: s.SourceID, Key: s.Key, Title: s.Title, Text: s.Text, Location: s.Location, Also: s.Also,
		Score: s.Score, RRF: s.RRF, AxisMult: s.AxisMult, Grounding: s.Grounding,
		VectorRank: s.VectorRank, KeywordRank: s.KeywordRank, Pinned: s.Pinned, Reason: s.Reason, Tokens: s.Tokens,
	}
}
