package infra

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// StackTemplate is a built-in stack with the profiles it needs (forge
// 22j), for a configuration to copy with infra.config.put: the stack owns
// its profiles, and a placement chooses only where and how many
// (question 1 as proposed, Tony to confirm).
type StackTemplate struct {
	Stack    Stack     `json:"stack"`
	Profiles []Profile `json:"profiles"`
}

// StackTemplates are the built-in stacks. The NATS cluster is not one: its
// nodes come with each site's cluster (natsNode, 22h), sized by the
// cluster's natsReplicas.
func StackTemplates() []StackTemplate {
	serve := func(id, name string, services ...string) Profile {
		return Profile{ID: id, Name: name, Kind: "container", Packages: []string{"axiom-server", "axiomd"},
			Units: []Unit{{Name: "serve", Kind: "serve", Services: services}}}
	}
	return []StackTemplate{
		{Stack: Stack{ID: "server", Name: "Server", Description: "The web entry points and the data services: what every deployment needs once", Profiles: []string{"server-web", "server-data"}},
			Profiles: []Profile{serve("server-web", "Server: web", "web"), serve("server-data", "Server: data", "data")}},
		{Stack: Stack{ID: "runners", Name: "Runners", Description: "AI calls and the index, placed where the work is", Profiles: []string{"runner"}},
			Profiles: []Profile{serve("runner", "Runner", "runner")}},
		{Stack: Stack{ID: "mail-workers", Name: "Mail workers", Description: "Runners plus the mail connector adapters", Profiles: []string{"mail-worker"}},
			Profiles: []Profile{serve("mail-worker", "Mail worker", "runner", "connectors:mail")}},
	}
}

func contributeStacks(reg *extensions.Registry) {
	RegisterToolAndCall(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "infra.stackTemplates", Source: source},
		Title:       "Built-in stacks",
		Description: "The built-in stacks (server, runners, mail workers), each {stack, profiles}: put the profiles and the stack into a configuration with infra.config.put, then place the stack with a placement {id, stack, sites: [{site, count}]}. Changes nothing.",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			if ps := rootOnly(call.Principal); ps != nil {
				return tools.Result{}, fmt.Errorf("%s", ps[0].Message)
			}
			if err := checkRead(call.ServiceArgs); err != nil {
				return tools.Result{}, err
			}
			content := map[string]any{}
			raw, _ := json.Marshal(map[string]any{"stacks": StackTemplates()})
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: fmt.Sprintf("%d built-in stack(s)", len(StackTemplates()))}, nil
		},
	})
}
