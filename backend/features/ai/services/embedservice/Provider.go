package embedservice

import "context"

// Provider is one AI vendor's embedding API.
type Provider interface {
	EmbedModel() ModelInfo
	// Embed embeds at most ModelInfo.MaxBatch texts. Errors should be
	// *providers.Error so they carry a status.
	Embed(ctx context.Context, in EmbedInput) (EmbedOutput, error)
}

type ModelInfo struct {
	Model      string
	Dimensions int
	MaxBatch   int
}

type EmbedInput struct {
	Model   string
	Texts   []string
	Purpose Purpose
	Title   string
}

type EmbedOutput struct {
	Vectors [][]float32
	Usage   Usage
}
