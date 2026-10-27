package impl

import (
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
)

// Complete returns the assistant's reply to args.Messages in one piece.
func (s *service) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	ctx, in, failed := s.input(&args)
	if failed != nil {
		return chatservice.CompleteResponse{Common: *failed}
	}
	out, err := s.provider.Complete(ctx, in)
	if err != nil {
		return chatservice.CompleteResponse{Common: *args.Fail(providers.Code(err), providers.Wrap(err))}
	}
	return response(args, in, out)
}
