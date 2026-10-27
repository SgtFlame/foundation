package impl

import (
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

type service struct {
	provider  embedservice.Provider
	batchSize int
}

// batch is the number of texts per provider call.
func (s *service) batch() int {
	n := s.batchSize
	if max := s.provider.EmbedModel().MaxBatch; max > 0 && (n <= 0 || max < n) {
		n = max
	}
	if n <= 0 {
		n = 1
	}
	return n
}

var log = providers.Log.Tags("embed")
