package services_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/services/catalog"
)

const module = "github.com/SgtFlame/foundation/backend"

// Every service's code takes only the proxies its manifest declares.
func TestManifestsDeclareTheirCalls(t *testing.T) {
	root, _ := filepath.Abs("../..")
	for _, s := range catalog.Services {
		dir := filepath.Join(root, strings.TrimPrefix(s.Interface.PkgPath(), module+"/"))
		problems, err := services.CheckCalls(dir, module, s.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// A service calling a proxy it does not declare fails, naming its manifest.
func TestUndeclaredCallFails(t *testing.T) {
	problems, err := services.CheckCalls("testdata/callsfixture/fixtureservice", module, services.Manifest{Name: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "fixture calls the config service") {
		t.Fatalf("problems %v", problems)
	}
	if problems, _ := services.CheckCalls("testdata/callsfixture/fixtureservice", module, services.Manifest{Name: "fixture", Calls: []string{"config"}}); len(problems) != 0 {
		t.Fatalf("a declared call: %v", problems)
	}
}

// The data services declare what they own; a process's permissions come
// from its set's manifests.
func TestProcessPermissions(t *testing.T) {
	byName := map[string]services.Spec{}
	for _, s := range catalog.Services {
		byName[s.Name] = s
		if s.Manifest.Name != s.Name {
			t.Errorf("%s: manifest named %q", s.Name, s.Manifest.Name)
		}
	}
	for _, owner := range []string{"identity", "authz", "config", "usage", "conversation", "chronicle", "retrieval", "cognition", "record", "live", "files", "connectors"} {
		if len(byName[owner].Manifest.Owns) == 0 {
			t.Errorf("%s declares no storage", owner)
		}
	}
	p := services.ProcessPermissions(catalog.Services, []string{"chat", "conversation"})
	if !slices.Contains(p.Subscribe, "svc.chat.>") || !slices.Contains(p.Subscribe, "svc.conversation.>") || slices.Contains(p.Subscribe, "svc.live.>") {
		t.Errorf("subscribe %v", p.Subscribe)
	}
	if slices.Contains(p.Publish, "evt.>") || slices.Contains(p.Publish, "$JS.>") {
		t.Errorf("a process without data services may reach storage: %v", p.Publish)
	}
	data := services.ProcessPermissions(catalog.Services, []string{"live", "files"})
	for _, want := range []string{"evt.>", "$JS.>", "$KV.>", "$O.FILES_>", "svc.live.>", "svc.files.>"} {
		if !slices.Contains(data.Subscribe, want) {
			t.Errorf("the data process lacks %s: %v", want, data.Subscribe)
		}
	}
}
