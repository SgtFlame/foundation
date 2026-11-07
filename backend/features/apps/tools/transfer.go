package apptools

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ContributeTransfer registers app.export, app.check and app.import (forge
// 05e); service returns the app service once made.
func ContributeTransfer(reg *extensions.Registry, service func() appservice.Service) {
	failed := func(message string, problems []live.Problem) error {
		if len(problems) == 0 {
			return fmt.Errorf("%s", message)
		}
		raw, _ := json.Marshal(problems)
		return fmt.Errorf("%s; problems: %s", message, raw)
	}

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "app.export", Source: "apps"}, Title: "Export an app", Effect: tools.EffectRead,
		Description: "Exports a published app version as one JSON document another tenant can import with app.import: its manifest, its own prefabs, graphs and entity types (parts), the tenant's parts it uses (tenantParts), the parts of apps it requires (appParts), the primitives they use, and with seed the records of its own entity types. Refused with the problems of the blueprints-only check when the app references anything else.",
		Input:       json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The app's id"},"version":{"type":"integer","description":"A published version; the last when left out"},"seed":{"type":"boolean","description":"Include the records of the app's own entity types as sample records"}},"required":["id"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				ID      string
				Version int
				Seed    bool
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := service().Export(appservice.ExportArgs{ServiceArgs: call.ServiceArgs, App: a.ID, Version: a.Version, Seed: a.Seed})
			if resp.Common.Code >= 300 {
				return tools.Result{}, failed(resp.Common.Message, resp.Problems)
			}
			return tools.Result{Content: map[string]any{"document": resp.Document},
				Summary: fmt.Sprintf("Exported %s version %d", a.ID, resp.Document.Version)}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "app.check", Source: "apps"}, Title: "Check an app document", Effect: tools.EffectRead,
		Description: "Checks an exported app document against this server: that it is whole, and that it references only primitives (components, node types, built-in graphs) and its own parts. Gives every problem with its path and a fix.",
		Input:       json.RawMessage(`{"type":"object","properties":{"document":{"type":"object","description":"A document as app.export gives it"}},"required":["document"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Document apps.Document }](call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := service().Check(appservice.CheckArgs{ServiceArgs: call.ServiceArgs, Document: a.Document})
			if resp.Common.Code >= 300 {
				return tools.Result{}, failed(resp.Common.Message, nil)
			}
			return tools.Result{Content: map[string]any{"problems": resp.Problems}, Summary: strconv.Itoa(len(resp.Problems)) + " problems"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "app.import", Source: "apps"}, Title: "Import an app", Effect: tools.EffectWrite,
		Description: "Imports an app document (from app.export) into this tenant as a draft and a published version, ready for app.install. The app keeps its id unless id is given; it is refused when this tenant has an app of that id. Every part keeps its id: when this tenant already has a different part of that id, nothing is imported until you decide it, so ask the user, showing both sides' versions and dates from the plan, then call again with overwrite (import the document's) or keep (leave this tenant's), naming parts as <type>:<id>. dryRun gives only the plan.",
		Input:       json.RawMessage(`{"type":"object","properties":{"document":{"type":"object","description":"A document as app.export gives it"},"id":{"type":"string","description":"The app's id here, when not the document's"},"overwrite":{"type":"array","items":{"type":"string"},"description":"Parts to overwrite, such as prefab:recordTable"},"keep":{"type":"array","items":{"type":"string"},"description":"Parts whose tenant version to keep"},"dryRun":{"type":"boolean"}},"required":["document"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct {
				Document        apps.Document
				ID              string
				Overwrite, Keep []string
				DryRun          bool
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			resp := service().Import(appservice.ImportArgs{ServiceArgs: call.ServiceArgs, Document: a.Document, ID: a.ID, Overwrite: a.Overwrite, Keep: a.Keep, DryRun: a.DryRun})
			content := map[string]any{"imported": resp.Imported, "plan": resp.Plan, "problems": resp.Problems}
			switch {
			case resp.Imported:
				content["version"] = resp.Version
				return tools.Result{Content: content, Summary: fmt.Sprintf("Imported %s as version %d; install it with app.install", resp.Plan.App, resp.Version)}, nil
			case resp.Common.Code < 300:
				return tools.Result{Content: content, Summary: fmt.Sprintf("The plan for importing %s", resp.Plan.App)}, nil
			}
			return tools.Result{}, failed(resp.Common.Message, resp.Problems)
		},
	})
}
