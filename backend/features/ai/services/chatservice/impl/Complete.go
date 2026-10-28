package impl

import (
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
)

// Complete returns the assistant's reply to args.Messages in one piece.
func (s *service) Complete(args chatservice.CompleteArgs) chatservice.CompleteResponse {
	ctx, in, failed := s.input(&args)
	if failed != nil {
		debugFailed(args, failed)
		return chatservice.CompleteResponse{Common: *failed}
	}
	debugInput(args, in, false)
	out, err := s.provider.Complete(ctx, in)
	if err != nil {
		failed := args.Fail(providers.Code(err), providers.Wrap(err))
		debugFailed(args, failed)
		return chatservice.CompleteResponse{Common: *failed}
	}
	return response(args, in, out)
}
