package impl

import (
	"context"

	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
)

// Stream generates the reply in the background, sending each piece on Deltas.
// Cancelling args.Ctx stops generation.
func (s *service) Stream(args chatservice.StreamArgs) *chatservice.StreamRequest {
	deltas := make(chan chatservice.Delta, 64)
	req := &chatservice.StreamRequest{Deltas: deltas, Response: make(chan *chatservice.CompleteResponse, 1)}

	ctx, in, failed := s.input(&args)
	if failed != nil {
		close(deltas)
		req.Response <- &chatservice.CompleteResponse{Common: *failed}
		return req
	}
	go func() {
		out, err := s.provider.Stream(ctx, in, func(d chatservice.Delta) error {
			select {
			case deltas <- d:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		})
		close(deltas)
		if err != nil {
			req.Response <- &chatservice.CompleteResponse{Common: *args.Fail(providers.Code(err), providers.Wrap(err))}
			return
		}
		resp := response(args, in, out)
		req.Response <- &resp
	}()
	return req
}
