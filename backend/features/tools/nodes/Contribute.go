// Package toolsnodes contributes the Cognition Agent node
// (documentation/plans/Cognition.md, Tools.md): the tool loop as a node.
// It runs unattended, so its write tools are denied (they need the user's
// approval) and it has no client tools.
package toolsnodes

import (
	"encoding/json"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
	"gorm.io/gorm"
)

// AgentID is the Agent node's type ID.
const AgentID = "llm.agent"

// Deps are what the node runs with, created after the registry.
type Deps struct {
	DB       *gorm.DB
	Chat     chatservice.Service
	Config   config.Reader
	Registry *extensions.Registry // where the tools are
}

// AgentConfig is an Agent node's config.
type AgentConfig struct {
	Level string   `json:"level"` // an ai.chat.levels ID; the default level when empty
	Tools []string `json:"tools"` // the tools it may call; empty allows every server tool that does not ask
}

// Contribute registers the Agent node.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: AgentID, Order: 300, Source: "tools"},
		Title:       "Agent",
		Category:    "LLM",
		Description: "Asks the model, letting it call tools until it answers. Tools that change data are left out, and denied when listed: a run cannot ask for approval.",
		Config:      json.RawMessage(agentSchema),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What to ask"),
				{ID: "system", Name: "System", Type: "string", Optional: true, Description: "The instructions"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("reply", "Reply", "string", "The model's answer"),
				cognition.DataPin("calls", "Calls", "json", "The tool calls it made: name, status and summary"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			prompt, err := cognition.Get[string](in, "prompt")
			if err != nil {
				return nil, "", err
			}
			system, err := cognition.Get[string](in, "system")
			if err != nil {
				return nil, "", err
			}
			var cfg AgentConfig
			if len(x.Node.Config) > 0 {
				if err := json.Unmarshal(x.Node.Config, &cfg); err != nil {
					return nil, "", tools.Log.Code("bad_agent_config").Wrap(err)
				}
			}
			d := deps()
			if d.Chat == nil {
				return nil, "", tools.Log.Code("agent_not_configured").New("the Agent node has no chat service")
			}
			settings := toolssettings.Defaults
			if d.Config != nil {
				if s, err := config.Read[toolssettings.Tools](d.Config, x.ServiceArgs, toolssettings.ID); err == nil {
					settings = s
				}
				if levels, err := config.Read[aisettings.Levels](d.Config, x.ServiceArgs, aisettings.LevelsID); err == nil && cfg.Level == "" {
					if l, ok := levels.Find(""); ok {
						cfg.Level = l.ID
					}
				}
			}
			offered := tools.Enabled(tools.Tools.All(d.Registry), nil, settings)
			// Writes listed by name are offered, and denied, so the trace says why
			offered = slices.DeleteFunc(offered, func(t tools.Tool) bool {
				if len(cfg.Tools) == 0 {
					return t.Effect.Asks()
				}
				return !slices.Contains(cfg.Tools, t.ID)
			})
			sa := x.ServiceArgs
			sa.Meta = sa.Meta.WithFeature("agent")
			out := tools.Loop{DB: d.DB, Chat: d.Chat}.Run(tools.RunArgs{
				CompleteArgs: chatservice.CompleteArgs{ServiceArgs: sa, Level: cfg.Level, System: system,
					Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: prompt}}},
				Tools:    offered,
				Settings: settings,
			})
			if c := out.Response.Common; c.Code >= 300 {
				return nil, "", tools.Log.Code("agent_failed").With("status", c.Code).Errorf("the model failed: %s", c.Message)
			}
			calls := make([]map[string]any, len(out.Calls))
			for i, c := range out.Calls {
				calls[i] = map[string]any{"name": c.Name, "status": c.Status, "summary": c.Summary, "error": c.Error}
			}
			return cognition.Values{"reply": out.Reply, "calls": calls}, cognition.Then, nil
		},
	})
}

const agentSchema = `{
  "type": "object",
  "properties": {
    "level": {"type": "string", "title": "Level", "ref": "ai.chat.levels#levels"},
    "tools": {"type": "array", "title": "Tools", "description": "The tools it may call, e.g. kb.search; empty allows every tool that does not ask", "items": {"type": "string"}}
  }
}`
