package tools

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Sources is the tools.source point (forge 07j): tools a tenant has beyond
// the server's own, such as its installed apps' (mail.readThread), found
// when a round or run asks for its tools.
var Sources = extensions.NewPoint[Source](
	"tools.source",
	"Tools per tenant beyond tools.tool, such as installed apps' manifest tools",
)

// Source gives the tools of the principal's tenant.
type Source struct {
	extensions.Descriptor
	Tools func(args features.ServiceArgs) []Tool
}

// Available is every server tool the principal's tenant has: the tools.tool
// contributions, then each source's, a name already taken left out.
func Available(reg *extensions.Registry, args features.ServiceArgs) []Tool {
	out := Tools.All(reg)
	seen := map[string]bool{}
	for _, t := range out {
		seen[t.ID] = true
	}
	for _, s := range Sources.All(reg) {
		if s.Tools == nil {
			continue
		}
		for _, t := range s.Tools(args) {
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
	}
	return out
}
