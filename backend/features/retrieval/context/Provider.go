// Package retrievalcontext is the RAG Search chat.contextProvider
// (documentation/ChatCompletion.md › ChatContextSettingsContributor):
// it runs retrieval for the round's message with the conversation's dials,
// and hands Send the segments to inject and cite.
package retrievalcontext

import (
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
)

// ID pairs the provider with the browser's RAG Search control.
const ID = "rag"

var log = logging.In("retrieval")

// Settings are the provider's settings for a conversation.
type Settings struct {
	Enabled          *bool           `json:"enabled"`
	Preset           string          `json:"preset"` // the chip last chosen; "" is Custom
	LexicalSemantic  int             `json:"lexicalSemantic"`
	ChronicleLibrary int             `json:"chronicleLibrary"`
	TopK             int             `json:"topK"`
	Sources          map[string]bool `json:"sources"` // a source left out is searched
}

// Contribute registers the provider. retriever returns the retrieval
// service when a round needs it: the service is created after the registry
// is complete, since configuration reads the registry first.
func Contribute(reg *extensions.Registry, retriever func() retrievalservice.Service) {
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: ID, Order: 100, Source: "retrieval"},
		Title:      "RAG Search",
		Schema:     json.RawMessage(schema),
		Defaults:   json.RawMessage(`{"enabled": true, "preset": "", "lexicalSemantic": 0, "chronicleLibrary": 0, "topK": 8, "sources": {"library": true, "chronicle": true}}`),
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			return gather(args, retriever())
		},
	})
}

func gather(args conversation.GatherArgs, service retrievalservice.Service) ([]conversation.ContextItem, error) {
	if service == nil {
		return nil, log.Code("retrieval_not_configured").New("retrieval is not configured")
	}
	var s Settings
	if len(args.Settings) > 0 {
		if err := json.Unmarshal(args.Settings, &s); err != nil {
			return nil, log.Code("bad_settings").Wrapf(err, "the RAG Search settings are not valid")
		}
	}
	var sources []string
	if len(s.Sources) > 0 {
		sources = []string{"none"} // every source turned off searches nothing
		for id, on := range s.Sources {
			if on {
				sources = append(sources, id)
			}
		}
	}
	resp := service.Retrieve(retrievalservice.RetrieveArgs{
		ServiceArgs: args.ServiceArgs, Query: args.Text,
		LexicalSemantic: clamp(s.LexicalSemantic, -5, 5), ChronicleLibrary: clamp(s.ChronicleLibrary, -5, 5), TopK: clamp(s.TopK, 0, 50),
		ContextWindow: args.ContextWindow, Sources: sources,
	})
	if resp.Common.Code != http.StatusOK {
		return nil, log.Code(resp.Common.ErrorCode).Errorf("retrieval failed with %d: %s", resp.Common.Code, resp.Common.Message)
	}
	items := make([]conversation.ContextItem, 0, len(resp.Segments))
	for _, seg := range resp.Segments {
		seg := seg
		items = append(items, conversation.ContextItem{
			Kind: "segment", Ref: seg.SourceID + ":" + seg.Key, Title: seg.Title, Text: seg.Text, Tokens: seg.Tokens,
			Score: seg.Score, Segment: &seg,
		})
	}
	return items, nil
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

const schema = `{
  "type": "object",
  "properties": {
    "enabled": {"type": "boolean", "title": "Search Volumes for context", "description": "Finds the passages of Volumes and Chronicle entries that best match each message and adds them, numbered, so the reply can cite them."},
    "preset": {"type": "string", "title": "Preset", "ref": "chat.context.presets#presets", "description": "The preset the dials start from; empty is Custom"},
    "lexicalSemantic": {"type": "integer", "title": "Lexical ↔ Semantic", "min": -5, "max": 5, "description": "−5 favours exact keywords (names, endpoints, error codes); +5 favours meaning"},
    "chronicleLibrary": {"type": "integer", "title": "Chronicles ↔ Library", "min": -5, "max": 5, "description": "−5 favours Chronicle memory; +5 favours Volumes"},
    "topK": {"type": "integer", "title": "Segments", "min": 1, "max": 50, "description": "How many passages are injected at most"},
    "sources": {"type": "object", "title": "Sources", "properties": {
      "library": {"type": "boolean", "title": "Volumes"},
      "chronicle": {"type": "boolean", "title": "Chronicles"}
    }}
  }
}`
