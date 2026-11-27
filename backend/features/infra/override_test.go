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

func TestTenantCluster(t *testing.T) {
	doc := sample()
	doc.Sites = append(doc.Sites, infra.Site{ID: "acme", Name: "Acme", Account: "test", Region: "r2"})
	doc.Clusters = append(doc.Clusters, infra.Cluster{ID: "acme", Site: "acme"})
	doc.HostPools = append(doc.HostPools, infra.HostPool{ID: "a", Site: "acme", Size: "m", Count: 1})
	doc.Overrides = []infra.Override{{ID: "acme", Tenant: 42, Spec: json.RawMessage(`{"cluster":{"site":"acme"},"runners":{"profile":"web","sites":[{"site":"acme","count":1}]}}`)}}
	if ps := doc.Check(); len(ps) != 0 {
		t.Fatalf("check = %+v", ps)
	}
	desired, kinds := infra.Expand(kindsReg(), doc)
	if p := infra.Plan(desired, kinds, infra.Actual{}); len(p.Refusals) != 0 {
		t.Fatalf("the tenant's runners on its own cluster: %+v", p.Refusals)
	}
	// A shared placement on the tenant's own cluster is refused.
	doc.Placements[0].Sites = append(doc.Placements[0].Sites, infra.SiteCount{Site: "acme", Count: 1})
	desired, kinds = infra.Expand(kindsReg(), doc)
	p := infra.Plan(desired, kinds, infra.Actual{})
	if len(p.Refusals) != 1 || p.Refusals[0].Resource != "instance:acme/web/web/1" || !strings.Contains(p.Refusals[0].Reason, "tenant 42's own") {
		t.Fatalf("refusals = %+v", p.Refusals)
	}
	// The tenant's runners elsewhere, a site with no cluster, and a second tenant on the same site are refused by the check.
	for spec, path := range map[string]string{
		`{"cluster":{"site":"acme"},"runners":{"profile":"web","sites":[{"site":"west","count":1}]}}`: "runners.sites.0.site",
		`{"cluster":{"site":"nowhere"}}`: "cluster.site",
	} {
		doc.Overrides[0].Spec = json.RawMessage(spec)
		if ps := doc.Check(); len(ps) == 0 || !strings.Contains(ps[len(ps)-1].Path, path) {
			t.Errorf("%s: %+v", spec, ps)
		}
	}
	doc.Overrides = []infra.Override{{ID: "a", Tenant: 42, Spec: json.RawMessage(`{"cluster":{"site":"acme"}}`)}, {ID: "b", Tenant: 43, Spec: json.RawMessage(`{"cluster":{"site":"acme"}}`)}}
	if ps := doc.Check(); len(ps) != 1 || !strings.Contains(ps[0].Message, "tenant 42's own already") {
		t.Errorf("two tenants on one cluster: %+v", ps)
	}
}

func TestTenantStreams(t *testing.T) {
	doc := sample()
	doc.Overrides = []infra.Override{{ID: "acme", Tenant: 42, Spec: json.RawMessage(`{"streams":{"site":"west"}}`)}}
	if ps := doc.Check(); len(ps) != 0 {
		t.Fatalf("check = %+v", ps)
	}
	desired, kinds := infra.Expand(kindsReg(), doc)
	p := infra.Plan(desired, kinds, infra.Actual{})
	var s infra.Step
	for _, st := range p.Steps {
		if st.Kind == infra.KindStreams {
			s = st
		}
	}
	if s.ID != "streams:42" || s.Desired["cluster"] != "west" || s.Desired["tenant"] != "42" || len(s.After) != 3 {
		t.Fatalf("the streams step = %+v", s)
	}
	doc.Overrides[0].Spec = json.RawMessage(`{"streams":{"site":"nowhere"}}`)
	if ps := doc.Check(); len(ps) == 0 {
		t.Fatal("streams at a site with no cluster were not refused")
	}
}
