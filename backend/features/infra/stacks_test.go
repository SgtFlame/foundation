package infra_test

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/services"
	"github.com/SgtFlame/foundation/backend/features/services/catalog"
)

// The built-in stacks put into a configuration check and are checked
// against the real services' manifests (forge 22j). No manifest declares
// a call to another service yet (services.CheckCalls keeps Calls in step
// with the proxies a service's code takes), so nothing is refused;
// TestServeUnits covers the refusal.
func TestStackTemplates(t *testing.T) {
	doc := sample()
	doc.Placements = nil
	for _, st := range infra.StackTemplates() {
		doc.Profiles = append(doc.Profiles, st.Profiles...)
		doc.Stacks = append(doc.Stacks, st.Stack)
	}
	doc.Placements = []infra.Placement{{ID: "runners", Stack: "runners", Sites: []infra.SiteCount{{Site: "west", Count: 2}}}}
	if ps := doc.Check(); len(ps) != 0 {
		t.Fatalf("check = %+v", ps)
	}
	sv := infra.Services{Specs: catalog.Services, Groups: services.FoundationGroups()}
	doc.Placements = append(doc.Placements, infra.Placement{ID: "server", Stack: "server", Sites: []infra.SiteCount{{Site: "west", Count: 1}}})
	access, refusals := infra.CheckUnits(doc, sv)
	if len(refusals) != 0 {
		t.Fatalf("with the server stack: %+v", refusals)
	}
	if a := access["runner"]; len(a) != 1 || strings.Join(a[0].Services, ",") != "chat,embed,index" {
		t.Fatalf("the runner's access = %+v", a)
	}
	desired, _ := infra.Expand(kindsReg(), doc)
	n := 0
	for _, r := range desired {
		if r.Kind == infra.KindInstance {
			n++
		}
	}
	if n != 4 { // two runners, a web and a data instance
		t.Fatalf("%d instances", n)
	}
}
