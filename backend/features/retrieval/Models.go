package retrieval

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
)

// RoundSegment is one segment a chat round injected, with the numbers that
// explain its choice, so the round's Context inspector and citations can be
// shown later and feedback on the round reaches the segment.
type RoundSegment struct {
	db.Model
	db.Tenanted
	RoundID     uint64 `gorm:"index"`
	Number      int    // its citation in the round, [n]
	SourceID    string
	SegmentKey  string // the feedback key
	OtherKeys   string // space-separated feedback keys of neighbours merged into it
	Title       string
	Snippet     string
	Location    string // JSON Location
	Also        string // newline-separated paths holding the same text
	Rank        int
	Score       float64
	RRF         float64 `gorm:"column:rrf"`
	AxisMult    float64
	Grounding   float64
	VectorRank  int
	KeywordRank int
	Pinned      bool
	Reason      string // why a pinned segment is in, e.g. "checked" or "stage 1"
	Tokens      int
}

// Keys are every feedback key the segment covers.
func (r RoundSegment) Keys() []string {
	return append([]string{r.SegmentKey}, strings.Fields(r.OtherKeys)...)
}

// RoundSegmentFrom records a selected segment for round, its snippet cut to
// snippetBytes.
func RoundSegmentFrom(round uint64, tenant db.Tenanted, s Scored) RoundSegment {
	loc, _ := json.Marshal(s.Location)
	other := ""
	if len(s.Keys) > 1 {
		other = strings.Join(s.Keys[1:], " ")
	}
	return RoundSegment{
		Tenanted: tenant, RoundID: round, Number: s.Number, SourceID: s.SourceID, SegmentKey: s.Key, OtherKeys: other,
		Title: s.Title, Snippet: Snippet(s.Text, snippetBytes), Location: string(loc), Also: strings.Join(s.Also, "\n"),
		Rank: s.Number, Score: s.Score, RRF: s.RRF, AxisMult: s.AxisMult, Grounding: s.Grounding,
		VectorRank: s.VectorRank, KeywordRank: s.KeywordRank, Pinned: s.Pinned, Reason: s.Reason, Tokens: s.Tokens,
	}
}

// SegmentFeedback counts the 👍 and 👎 of the rounds that injected a
// segment, per tenant. A library chunk's key is its content hash, so its
// feedback survives reindexing while its text is unchanged.
type SegmentFeedback struct {
	db.Model
	db.Tenanted
	SourceID   string `gorm:"index:segment_feedback_key"`
	SegmentKey string `gorm:"index:segment_feedback_key"`
	Up, Down   int
	LastAt     time.Time
}

// TableName keeps the plan's name for the table.
func (SegmentFeedback) TableName() string { return "segment_feedback" }

// Models are the retrieval feature's tables, for db.Migrate.
func Models() []any {
	return []any{&RoundSegment{}, &SegmentFeedback{}}
}

// snippetBytes is the length of a recorded segment's snippet.
const snippetBytes = 400

// Snippet is text with its whitespace collapsed, cut to about n bytes on a
// rune boundary.
func Snippet(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= n {
		return text
	}
	for n > 0 && (text[n]&0xC0) == 0x80 {
		n--
	}
	return text[:n] + "…"
}
