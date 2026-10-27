package embedservice

import (
	"github.com/SgtFlame/foundation/backend/features"
)

// Service turns text into vector embeddings. It is provider-neutral: the
// provider (Gemini, OpenAI, Azure AI, ...) is chosen when the service is created.
type Service interface {
	Embed(args EmbedArgs) EmbedResponse
	Describe(args DescribeArgs) DescribeResponse
}

// Purpose tells providers that embed documents and queries differently (such
// as Gemini's task types) which side of a search a text is on. Providers
// without the distinction ignore it.
type Purpose string

const (
	PurposeDocument Purpose = "document"
	PurposeQuery    Purpose = "query"
)

type EmbedArgs struct {
	features.ServiceArgs
	Texts   []string
	Purpose Purpose
	Title   string // optional title of the document the texts come from
	Model   string // optional; the provider's default when empty
}

type Usage struct {
	InputTokens int
}

type EmbedResponse struct {
	Common     features.ServiceResponse
	Vectors    [][]float32 // one unit-length vector per text, in order
	Model      string
	Dimensions int
	Usage      Usage
}

type DescribeArgs struct {
	features.ServiceArgs
}

// DescribeResponse describes the default model, so that stored embeddings can
// tell when they were made by a different one.
type DescribeResponse struct {
	Common     features.ServiceResponse
	Model      string
	Dimensions int
	MaxBatch   int // texts per provider call
}
