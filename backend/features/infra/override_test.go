package infra_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/infratest"
)

// A tenant override's runners are the tenant's own instances; asking for
// VMs refuses a site whose hosts cannot run them (forge 22k).
func TestTenantRunners(t *testing.T) {
	doc := sample()
	doc.Overrides = []infra.Override{{ID: "acme", Tenant: 42, Spec: json.RawMessage(`{"runners":{"profile":"web","sites":[{"site":"west","count":2}],"vm":true}}`)}}
	if ps := doc.Check(); len(ps) != 0 {
		t.Fatalf("check = %+v", ps)
	}
	desired, kinds := infra.Expand(kindsReg(), doc)
	var own []infra.Resource
	for _, r := range desired {
		if strings.Contains(r.ID, "tenant-42-acme") {
			own = append(own, r)
		}
	}
	if len(own) != 2 || own[0].ID != "instance:west/tenant-42-acme/web/1" || own[0].Props["tenant"] != "42" || own[0].Props["kind"] != "vm" || !own[0].NeedsVMs {
		t.Fatalf("the tenant's instances = %+v", own)
	}
	sizes := infra.Actual{Sizes: map[string][]infra.ProviderSize{"test": infratest.Sizes}}
	p := infra.Plan(desired, kinds, sizes)
	if len(p.Refusals) != 2 || !strings.Contains(p.Refusals[0].Reason, "no host on west can run VMs") || p.Refusals[0].Resource != "instance:west/tenant-42-acme/web/1" {
		t.Fatalf("refusals = %+v", p.Refusals)
	}
	doc.HostPools[0].Size = "l" // l runs VMs
	desired, kinds = infra.Expand(kindsReg(), doc)
	if p := infra.Plan(desired, kinds, sizes); len(p.Refusals) != 0 {
		t.Fatalf("on hosts that run VMs: %+v", p.Refusals)
	}
	// Bad overrides are refused by the check.
	for _, spec := range []string{`{"runners":{"sites":[]}}`, `{"runners":{"profile":"nope","sites":[]}}`, `{"database":{}}`} {
		doc.Overrides[0].Spec = json.RawMessage(spec)
		if ps := doc.Check(); len(ps) == 0 {
			t.Errorf("%s was not refused", spec)
		}
	}
	doc.Overrides[0].Spec, doc.Overrides[0].Tenant = nil, 3
	if ps := doc.Check(); len(ps) != 1 || !strings.Contains(ps[0].Path, "tenant") {
		t.Errorf("an override of the root tenant: %+v", ps)
	}
}
