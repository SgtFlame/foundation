package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"gorm.io/gorm"
)

// VoteOnce counts a verdict on segments once per key.
func (s *service) VoteOnce(args retrievalservice.VoteOnceArgs) retrievalservice.VoteOnceResponse {
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, "retrieval.vote", resource).Err("retrieval.vote", resource); err != nil {
		return retrievalservice.VoteOnceResponse{Common: *args.Fail(authz.Status, err)}
	}
	if v := retrieval.Feedback(args.Verdict); v != retrieval.FeedbackCorrect && v != retrieval.FeedbackIncorrect {
		return retrievalservice.VoteOnceResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_vote").Errorf("a vote is up or down, not %q", args.Verdict))}
	}
	if s.db == nil {
		return retrievalservice.VoteOnceResponse{Common: *args.Fail(http.StatusServiceUnavailable, log.Code("votes_unavailable").New("votes cannot be stored here"))}
	}
	var cast bool
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		cast, err = retrieval.VoteOnce(tx, args.Principal.TenantID, args.Key, args.Segments, retrieval.Feedback(args.Verdict))
		return err
	})
	if err != nil {
		return retrievalservice.VoteOnceResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return retrievalservice.VoteOnceResponse{Common: features.OK(), Cast: cast}
}
