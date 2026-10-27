package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

// Describe reports the provider's default embedding model.
func (s *service) Describe(args embedservice.DescribeArgs) embedservice.DescribeResponse {
	if s.provider == nil {
		return embedservice.DescribeResponse{Common: *args.Fail(providers.ErrNotConfigured.Code, providers.Wrap(providers.ErrNotConfigured))}
	}
	info := s.provider.EmbedModel()
	return embedservice.DescribeResponse{
		Common:     features.OK(),
		Model:      info.Model,
		Dimensions: info.Dimensions,
		MaxBatch:   s.batch(),
	}
}
