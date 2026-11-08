package apptools_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	apptools "github.com/SgtFlame/foundation/backend/features/apps/tools"
	chroniclenodes "github.com/SgtFlame/foundation/backend/features/chronicle/nodes"
	chroniclesteps "github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	conversationnodes "github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	recordsdata "github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/tools"
	toolsnodes "github.com/SgtFlame/foundation/backend/features/tools/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	"github.com/SgtFlame/foundation/backend/features/ui/hello"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// everything is a registry with every feature's components, node types and
// commands, as serve.go contributes them, and the forge.* tools.
func everything(t *testing.T) *extensions.Registry {
	reg := extensions.NewRegistry()
	reg.Declare(cognition.Nodes, cognition.Graphs, cognition.Types, cognition.Kinds, cognition.Conversions, tools.Tools,
		live.Commands, live.Projectors, live.Queries, live.Calls, live.Extensions, ui.Components)
	cognitionnodes.Contribute(reg)
	conversationnodes.Contribute(reg)
	chroniclenodes.Contribute(reg, func() chroniclesteps.Steps { return chroniclesteps.Steps{} })
	toolsnodes.Contribute(reg, func() toolsnodes.Deps { return toolsnodes.Deps{} })
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	drafts.Contribute(reg, func() drafts.Deps { return drafts.Deps{} })
	livenodes.Contribute(reg, func() livenodes.Deps { return livenodes.Deps{} })
	uinodes.Contribute(reg)
	hello.Contribute(reg)
	entities.Contribute(reg, func() entities.Deps { return entities.Deps{} })
	recordsdata.Contribute(reg, func() recordsdata.Deps { return recordsdata.Deps{} })
	recordsnodes.Contribute(reg, func() recordsdata.Deps { return recordsdata.Deps{} })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabs.Deps{} })
	manifests.Contribute(reg, func() manifests.Deps { return manifests.Deps{} })
	installs.Contribute(reg, func() installs.Deps { return installs.Deps{} })
	ops.Contribute(reg)
	ops.ContributeUndo(reg, func() ops.Deps { return ops.Deps{} })
	apptools.ContributeForge(reg, func() apptools.ForgeDeps { return apptools.ForgeDeps{} })
	apptools.ContributeApply(reg, func() apptools.ForgeDeps { return apptools.ForgeDeps{} })
	apptools.ContributeTest(reg, func() apptools.ForgeDeps { return apptools.ForgeDeps{} })
	return reg
}

// Every catalog item and op carries the metadata a model needs (forge
// 06d): components and node types a title, description, tags and an
// example that fits; every command a description of each field and an
// example its schema accepts. Field types are checked in records.
func TestMetadata(t *testing.T) {
	reg := everything(t)
	for _, c := range ui.Components.All(reg) {
		if c.Title == "" || c.Description == "" || c.Category == "" || len(c.Tags) == 0 || len(c.Examples) == 0 {
			t.Errorf("component %s lacks a title, description, category, tags or an example", c.ID)
		}
	}
	for _, n := range cognition.Nodes.All(reg) {
		if p := cognition.MetaProblems(n); len(p) > 0 || n.Title == "" || len(cognition.TagsOf(n)) == 0 {
			t.Errorf("node type %s lacks a title, tags or %v", n.ID, p)
		}
		var ex struct {
			Type   string
			Config map[string]any
		}
		if err := json.Unmarshal(cognition.ExamplesOf(n)[0], &ex); err != nil || ex.Type != n.ID {
			t.Errorf("node type %s's example is not {type: %q, config}: %v", n.ID, n.ID, err)
			continue
		}
		for _, r := range cognition.ConfigRequired(n.Config) {
			if _, ok := ex.Config[r]; !ok {
				t.Errorf("node type %s's example has no %s, which its config requires: give it Examples", n.ID, r)
			}
		}
		raw, _ := json.Marshal(ex.Config)
		if _, err := n.PinsOf(raw); err != nil {
			t.Errorf("node type %s's example config: %v", n.ID, err)
		}
	}
	for _, c := range live.Commands.All(reg) {
		if c.Internal {
			continue
		}
		var s struct {
			Properties map[string]struct{ Description string }
		}
		if err := json.Unmarshal(c.Args, &s); err != nil {
			t.Errorf("%s's args schema: %v", c.ID, err)
		}
		for k, p := range s.Properties {
			if p.Description == "" {
				t.Errorf("%s's field %s has no description", c.ID, k)
			}
		}
		if len(c.Examples) == 0 {
			t.Errorf("%s has no example", c.ID)
		} else if err := tools.CheckArgs(c.Args, c.Examples[0]); err != nil {
			t.Errorf("%s's example: %v", c.ID, err)
		}
	}
}

// run runs a registered tool.
func run(t *testing.T, reg *extensions.Registry, id string, args any) (tools.Result, error) {
	t.Helper()
	i := slices.IndexFunc(tools.Tools.All(reg), func(tl tools.Tool) bool { return tl.ID == id })
	if i < 0 {
		t.Fatalf("no tool %s", id)
	}
	raw, _ := json.Marshal(args)
	return tools.Tools.All(reg)[i].Run(tools.Call{ServiceArgs: features.ServiceArgs{Principal: features.LocalPrincipal()}, Args: raw})
}

// The catalog's areas without a live store: components, nodes, fields and
// the empty connectors; a search ranks title and tag matches first.
func TestCatalog(t *testing.T) {
	reg := everything(t)
	items := func(args map[string]any) []apptools.Item {
		t.Helper()
		res, err := run(t, reg, "forge.catalog", args)
		if err != nil {
			t.Fatalf("forge.catalog %v: %v", args, err)
		}
		return res.Content["items"].([]apptools.Item)
	}
	if got := items(map[string]any{"area": "fields"}); len(got) == 0 || got[0].Ref != "field:autoNumber" {
		t.Errorf("fields: %+v", got)
	}
	if got := items(map[string]any{"area": "connectors"}); len(got) != 0 {
		t.Errorf("connectors: %+v", got)
	}
	got := items(map[string]any{"search": "money"})
	if len(got) == 0 || got[0].Ref != "field:money" {
		t.Errorf("money: %+v", got)
	}
	got = items(map[string]any{"area": "nodes", "search": "branch"})
	if !slices.ContainsFunc(got, func(it apptools.Item) bool { return it.Ref == "node:flow.branch" }) {
		t.Errorf("branch: %+v", got)
	}
	for _, it := range items(map[string]any{"area": "components"}) {
		if !strings.HasPrefix(it.Ref, "component:") || it.Title == "" {
			t.Errorf("component item %+v", it)
		}
	}
	if _, err := run(t, reg, "forge.catalog", map[string]any{"area": "widgets"}); err == nil {
		t.Error("an unknown area was searched")
	}

	describe := func(ref string) map[string]any {
		t.Helper()
		res, err := run(t, reg, "forge.describe", map[string]any{"ref": ref})
		if err != nil {
			t.Fatalf("forge.describe %s: %v", ref, err)
		}
		return res.Content
	}
	if d := describe("component:mui.button"); d["props"] == nil || d["examples"] == nil {
		t.Errorf("mui.button: %v", d)
	}
	if d := describe("node:records.get"); !strings.Contains(string(d["examples"].([]json.RawMessage)[0]), `"entity":"task"`) {
		t.Errorf("records.get: %v", d["examples"])
	}
	if d := describe("field:rollup"); d["computed"] != true {
		t.Errorf("rollup: %v", d)
	}
	for _, ref := range []string{"component:mui.nope", "node:nope", "field:nope", "connector:smtp", "button"} {
		if _, err := run(t, reg, "forge.describe", map[string]any{"ref": ref}); err == nil {
			t.Errorf("%s was described", ref)
		}
	}
}
