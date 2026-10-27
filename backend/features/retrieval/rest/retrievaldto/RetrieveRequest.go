package retrievaldto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/index/rest/indexdto"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

type RetrieveRequest struct {
	Query            string         `json:"query"`
	LexicalSemantic  int            `json:"lexicalSemantic"`
	ChronicleLibrary int            `json:"chronicleLibrary"`
	TopK             int            `json:"topK,omitempty"`
	ContextWindow    int            `json:"contextWindow,omitempty"`
	Sources          []string       `json:"sources,omitempty"`
	VolumeIDs        indexdto.TSIDs `json:"volumeIds,omitempty"`
}

// ToArgs converts the request to the service's RetrieveArgs.
func (r RetrieveRequest) ToArgs(args features.ServiceArgs) retrievalservice.RetrieveArgs {
	return retrievalservice.RetrieveArgs{
		ServiceArgs: args, Query: r.Query, LexicalSemantic: r.LexicalSemantic, ChronicleLibrary: r.ChronicleLibrary,
		TopK: r.TopK, ContextWindow: r.ContextWindow, Sources: r.Sources, VolumeIDs: r.VolumeIDs,
	}
}
