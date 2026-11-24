package retrieval

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Feedback is the user's verdict on a round's reply; the empty string is
// neutral.
type Feedback string

const (
	FeedbackNone      Feedback = ""
	FeedbackCorrect   Feedback = "up"     // 👍 the reply was grounded: its segments get +1 up
	FeedbackIncorrect Feedback = "down"   // 👎 retrieval or generation failed: its segments get +1 down, the round stays in the window
	FeedbackIgnore    Feedback = "ignore" // 🚫 the round leaves the window; segment counts are not touched
)

// Valid reports whether f is one of the four verdicts.
func (f Feedback) Valid() bool {
	switch f {
	case FeedbackNone, FeedbackCorrect, FeedbackIncorrect, FeedbackIgnore:
		return true
	}
	return false
}

// votes is what a verdict adds to each segment's counts.
func (f Feedback) votes() (up, down int) {
	switch f {
	case FeedbackCorrect:
		return 1, 0
	case FeedbackIncorrect:
		return 0, 1
	}
	return 0, 0
}

// Vote moves the counts of a round's segments from the round's previous
// verdict to its new one, so 👍 → 👎 moves the vote rather than adding a
// second. Run it in the transaction that stores the round's verdict.
func Vote(tx *gorm.DB, tenant uint64, segments []RoundSegment, from, to Feedback) error {
	fromUp, fromDown := from.votes()
	toUp, toDown := to.votes()
	dUp, dDown := toUp-fromUp, toDown-fromDown
	if dUp == 0 && dDown == 0 {
		return nil
	}
	now := time.Now()
	for _, s := range segments {
		for _, key := range s.Keys() {
			var f SegmentFeedback
			err := tx.Where("tenant_id = ? AND source_id = ? AND segment_key = ?", tenant, s.SourceID, key).First(&f).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				f = SegmentFeedback{SourceID: s.SourceID, SegmentKey: key}
				f.TenantID = tenant
			case err != nil:
				return err
			}
			f.Up, f.Down, f.LastAt = max(f.Up+dUp, 0), max(f.Down+dDown, 0), now
			if err := tx.Save(&f).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// SegmentRef names a segment by its source and feedback key.
type SegmentRef struct {
	SourceID string `json:"sourceId"`
	Key      string `json:"key"`
}

// VoteOnce casts verdict (up or down) on segments once for key in the
// tenant (forge 15b), as a 👍 or 👎 on a round would: a key that has voted
// already changes nothing, so a second outcome write does not count twice.
// It says whether the vote was cast. Run it in a transaction.
func VoteOnce(tx *gorm.DB, tenant uint64, key string, segments []SegmentRef, verdict Feedback) (bool, error) {
	if verdict != FeedbackCorrect && verdict != FeedbackIncorrect {
		return false, errors.New("a vote is up or down")
	}
	if strings.TrimSpace(key) == "" {
		return false, errors.New("a vote needs a key")
	}
	var n int64
	if err := tx.Model(&SegmentVote{}).Where("tenant_id = ? AND key = ?", tenant, key).Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	encoded, _ := json.Marshal(segments)
	v := SegmentVote{Key: key, Verdict: string(verdict), Segments: string(encoded)}
	v.TenantID = tenant
	if err := tx.Create(&v).Error; err != nil {
		return false, err
	}
	rs := make([]RoundSegment, 0, len(segments))
	for _, s := range segments {
		if s.SourceID != "" && s.Key != "" {
			rs = append(rs, RoundSegment{SourceID: s.SourceID, SegmentKey: s.Key})
		}
	}
	return true, Vote(tx, tenant, rs, FeedbackNone, verdict)
}

// GroundingSettings shape the grounding multiplier (the retrieval.feedback
// section).
type GroundingSettings struct {
	Gamma float64 `json:"gamma"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

// Grounding is clamp(((1 + up) / (1 + down))^γ, min, max): 1 with no votes,
// about 1.4× for one vote at γ = 0.5, saturating at max (or min).
func (g GroundingSettings) Grounding(up, down int) float64 {
	v := math.Pow(float64(1+up)/float64(1+down), g.Gamma)
	if g.Min > 0 {
		v = math.Max(v, g.Min)
	}
	if g.Max > 0 {
		v = math.Min(v, g.Max)
	}
	return v
}
