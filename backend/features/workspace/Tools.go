package workspace

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// contributeTools registers workspace.list, the model's read of the
// person's workspaces (forge 16a question 7). Making, renaming, moving and
// deleting are the commands' own tools; switching is the browser's
// workspace.switch, since which workspace a tab shows is the tab's.
func contributeTools(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "workspace.list", Source: source}, Title: "Your workspaces", Effect: tools.EffectRead,
		Description: "Lists your workspaces in the Workspaces menu's order, each {id, name, isDefault}. The one this browser tab shows is the workbench context's workspace. Make one with workspace.create, rename it with workspace.rename, delete it with workspace.delete (never Default) and switch this tab to one with workspace.switch.",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			d := deps()
			if d.Store == nil {
				return tools.Result{}, fmt.Errorf("workspaces are not available")
			}
			p := call.Principal
			if p.UserID == 0 {
				return tools.Result{}, fmt.Errorf("only people have workspaces")
			}
			all, err := All(call.Context(), d.Store, p.TenantID, p.UserID)
			if err != nil {
				return tools.Result{}, fmt.Errorf("your workspaces cannot be read")
			}
			list := []map[string]any{}
			var names []string
			for _, w := range all {
				list = append(list, map[string]any{"id": w.ID, "name": w.Name, "isDefault": w.IsDefault})
				names = append(names, w.Name)
			}
			summary := "No workspaces yet"
			if len(names) > 0 {
				summary = "Workspaces: " + strings.Join(names, ", ")
			}
			return tools.Result{Content: map[string]any{"workspaces": list}, Summary: summary}, nil
		},
	})
}
