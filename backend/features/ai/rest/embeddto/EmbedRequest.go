package embeddto

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
)

type EmbedRequest struct {
	Texts   []string `json:"texts"`
	Purpose string   `json:"purpose,omitempty"` // "document" (default) or "query"
	Title   string   `json:"title,omitempty"`
	Model   string   `json:"model,omitempty"`
}

// ToArgs converts the request to the service's EmbedArgs.
func (r EmbedRequest) ToArgs(args features.ServiceArgs) embedservice.EmbedArgs {
	return embedservice.EmbedArgs{
		ServiceArgs: args,
		Texts:       r.Texts,
		Purpose:     embedservice.Purpose(r.Purpose),
		Title:       r.Title,
		Model:       r.Model,
	}
}
