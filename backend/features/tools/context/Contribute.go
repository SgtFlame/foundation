// Package toolscontext registers the tools chat.contextProvider: it adds no
// context, but its "enabled" setting is the conversation's Tools switch,
// paired with the browser's chat.contextSettings control of the same ID,
// and its defaults section is Settings › AI › Chat › Context › Tools.
package toolscontext

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Contribute registers the provider.
func Contribute(reg *extensions.Registry) {
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: tools.ContextID, Order: 400, Source: "tools"},
		Title:      "Tools",
		Schema:     json.RawMessage(`{"type": "object", "properties": {"enabled": {"type": "boolean", "title": "Let the model call tools", "description": "Search and read Volumes and Chronicles, ask you to choose, open editors, and propose changes for you to approve"}}}`),
		Defaults:   json.RawMessage(`{"enabled": true}`),
	})
}
