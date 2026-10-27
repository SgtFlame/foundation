package retrievalservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Service finds the context for a chat round across every retrieval.source
// (documentation/ContextRetrieval.md): each source's vector and
// keyword rankings are fused by Reciprocal Rank Fusion weighted by the
// Lexical ↔ Semantic dial, multiplied by the Chronicle ↔ Library dial and by
// the grounding feedback has earned, then deduplicated and cut to Top-K and
// the token budget. Every step is deterministic, and the response explains
// every segment.
type Service interface {
	Retrieve(args RetrieveArgs) RetrieveResponse
}

type RetrieveArgs struct {
	features.ServiceArgs
	Query            string
	LexicalSemantic  int      // Axis 1, −5 (keywords only) … +5 (vectors only)
	ChronicleLibrary int      // Axis 2, −5 (favour Chronicle memory) … +5 (favour the library)
	TopK             int      // segments to inject, 1…50; 0 is the retrieval section's default
	ContextWindow    int      // the round's model's, in tokens; the budget is a share of it. 0 uses the largest budget
	Sources          []string // the source IDs to search; empty searches every source
	VolumeIDs        []uint64 // optional; only these volumes
}

type RetrieveResponse struct {
	Common       features.ServiceResponse
	Segments     []retrieval.Scored // in citation order, [1] first
	Candidates   int                // distinct segments found before selection
	BudgetTokens int
	// VectorSkipped says the vector searches did not run, though the dial
	// asked for them: embedding the query failed (no API key, say), so the
	// keyword ranking was used alone.
	VectorSkipped bool
}
