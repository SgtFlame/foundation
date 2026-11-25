package impl

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
)

// gather runs the named context providers enabled for the conversation
// (every provider with a Gather when none are named) concurrently, and
// returns their items in the providers' order. A provider that fails is
// skipped for the round.
func (s *service) gather(args conversationservice.SendArgs, r *round, named []string) []conversation.ContextItem {
	providers := conversation.ContextProviders.All(s.registry)
	results := make([][]conversation.ContextItem, len(providers))
	// The providers' own calls (query embeddings) are attributed to the round
	call := args.ServiceArgs
	call.Ctx = context.WithoutCancel(args.Context())
	call.Meta = features.Meta{ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	var wg sync.WaitGroup
	for i, p := range providers {
		if p.Gather == nil || (len(named) > 0 && !slices.Contains(named, p.ID)) {
			continue
		}
		settings := s.settingsOf(args, r, p.ID, p.Defaults)
		if !enabled(settings) {
			continue
		}
		ga := conversation.GatherArgs{
			ServiceArgs: call, ConversationID: r.conversation.ID, Text: r.round.Text, Window: r.history,
			Workbench: args.Workbench, Activity: r.activity, Settings: settings, ContextWindow: r.model.ContextWindow,
		}
		wg.Go(func() {
			items, err := p.Gather(ga)
			if err != nil {
				log.With("provider", p.ID).Warn(args.Context(), "context provider %s failed, so the round goes without it: %v", p.ID, err)
				return
			}
			results[i] = items
		})
	}
	wg.Wait()
	return slices.Concat(results...)
}

// settingsOf is a provider's settings for the conversation, else its
// defaults section, else the defaults it declared.
func (s *service) settingsOf(args conversationservice.SendArgs, r *round, id string, defaults json.RawMessage) json.RawMessage {
	if v, ok := r.settings[id]; ok {
		return v
	}
	if v, err := s.config.Resolved(args.ServiceArgs, conversation.DefaultsSection(id)); err == nil {
		return v
	}
	return defaults
}

// enabled is a provider's "enabled" setting; it is on unless set to false.
func enabled(settings json.RawMessage) bool {
	var v struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.Unmarshal(settings, &v)
	return v.Enabled == nil || *v.Enabled
}

// debugging is the debug provider's "enabled" setting, which, unlike the
// others, is off unless set to true.
func debugging(settings json.RawMessage) bool {
	var v struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(settings, &v)
	return v.Enabled
}
