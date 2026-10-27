package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"gorm.io/gorm"
)

// Feedback stores the verdict on one of the principal's rounds and moves
// its segments' counts from the previous verdict to the new one, in one
// transaction, then tells the round followers.
func (s *service) Feedback(args conversationservice.FeedbackArgs) conversationservice.FeedbackResponse {
	if failed := check(args.ServiceArgs, "conversation.write", 0); failed != nil {
		return conversationservice.FeedbackResponse{Common: *failed}
	}
	if !args.Feedback.Valid() {
		return conversationservice.FeedbackResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("bad_feedback").Errorf("feedback %q is not one of up, down, ignore or empty", args.Feedback))}
	}
	var round conversation.Round
	var from retrieval.Feedback
	errNotFound := errors.New("not found")
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("tenant_id = ? AND owner_id = ?", args.Principal.TenantID, args.Principal.UserID)
		if err := q.First(&round, args.RoundID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNotFound
			}
			return err
		}
		from = retrieval.Feedback(round.Feedback)
		if from == args.Feedback {
			return nil
		}
		var segments []retrieval.RoundSegment
		if err := tx.Where("tenant_id = ? AND round_id = ?", args.Principal.TenantID, round.ID).Find(&segments).Error; err != nil {
			return err
		}
		if err := retrieval.Vote(tx, args.Principal.TenantID, segments, from, args.Feedback); err != nil {
			return err
		}
		round.Feedback = string(args.Feedback)
		return tx.Model(&round).Update("feedback", round.Feedback).Error
	})
	switch {
	case errors.Is(err, errNotFound):
		return conversationservice.FeedbackResponse{Common: *args.Fail(http.StatusNotFound,
			log.Code("round_not_found").With("roundId", args.RoundID).New("there is no such round"))}
	case err != nil:
		return conversationservice.FeedbackResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if from != args.Feedback {
		for _, f := range conversation.RoundFollowers.All(s.registry) {
			if f.Feedback != nil {
				f.Feedback(args.ServiceArgs, round, from, args.Feedback)
			}
		}
	}
	return conversationservice.FeedbackResponse{Common: features.OK(), Round: round}
}
