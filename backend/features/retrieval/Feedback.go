package retrieval

import (
	"errors"
	"math"
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
