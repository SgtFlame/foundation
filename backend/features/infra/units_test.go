package infra_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/services"
)

func unitServices() infra.Services {
	return infra.Services{
		Specs: []services.Spec{
			{Name: "web", Manifest: services.Manifest{Calls: []string{"records"}}},
			{Name: "records", Manifest: services.Manifest{Secrets: []string{"config/records"}, Subjects: []string{"$KV.records_>"}}},
			{Name: "runner", Manifest: services.Manifest{Calls: []string{"records", "chat"}}},
			{Name: "chat", Manifest: services.Manifest{Secrets: []string{"server/gemini-api-key"}}},
		},
		Groups: services.Groups{"front": {"web"}},
	}
}

func unitDoc() infra.Document {
	return infra.Document{
		Accounts: []infra.Account{{ID: "test", Name: "Test", Provider: "infratest", Connection: "c1"}},
		Sites:    []infra.Site{{ID: "east", Name: "East", Account: "test", Region: "r1"}, {ID: "west", Name: "West", Account: "test", Region: "r2"}},
		Clusters: []infra.Cluster{{ID: "east", Site: "east"}, {ID: "west", Site: "west"}},
		HostPools: []infra.HostPool{{ID: "e", Site: "east", Size: "m", Count: 1}, {ID: "w", Site: "west", Size: "m", Count: 1}},
		Profiles: []infra.Profile{
			{ID: "front", Name: "Front", Kind: "container", Units: []infra.Unit{{Name: "serve", Kind: "serve", Services: []string{"front"}}}},
			{ID: "data", Name: "Data", Kind: "container", Units: []infra.Unit{{Name: "serve", Kind: "serve", Services: []string{"records"}}}},
		},
		Placements: []infra.Placement{
			{ID: "front", Profile: "front", Sites: []infra.SiteCount{{Site: "east", Count: 1}, {Site: "west", Count: 2}}},
			{ID: "data", Profile: "data", Sites: []infra.SiteCount{{Site: "east", Count: 1}}},
		},
	}
}

// A unit's calls must be served on its site, or across a gateway when
// crossSite allows; a unit's access comes from its services' manifests
// (forge 22i).
func TestServeUnits(t *testing.T) {
	doc := unitDoc()
	access, refusals := infra.CheckUnits(doc, unitServices())
	if len(refusals) != 1 || refusals[0].Resource != "instance:west/front/front/1" || refusals[0].Reason != "web calls records, which no unit serves on site west" ||
		!strings.Contains(refusals[0].Fix, "crossSite") {
		t.Fatalf("refusals = %+v", refusals)
	}
	d := access["data"]
	if len(d) != 1 || fmt.Sprint(d[0].Services) != "[records]" || fmt.Sprint(d[0].Secrets) != "[config/records]" ||
		!strings.Contains(fmt.Sprint(d[0].Permissions.Subscribe), "svc.records.>") || !strings.Contains(fmt.Sprint(d[0].Permissions.Publish), "$KV.records_>") {
		t.Fatalf("data's access = %+v", d)
	}
	if f := access["front"]; len(f) != 1 || !strings.Contains(fmt.Sprint(f[0].Permissions.Publish), "svc.records.>") || len(f[0].Secrets) != 0 {
		t.Fatalf("front's access = %+v", f)
	}
	// Allowed across the gateway: no refusal.
	doc.Settings.CrossSite = []string{"records"}
	if _, refusals := infra.CheckUnits(doc, unitServices()); len(refusals) != 0 {
		t.Fatalf("with crossSite: %+v", refusals)
	}
	// ... but not without a gateway (west has no cluster, so no NATS).
	doc.Clusters = doc.Clusters[:1]
	if _, refusals := infra.CheckUnits(doc, unitServices()); len(refusals) != 1 {
		t.Fatalf("without a gateway: %+v", refusals)
	}
	// A unit naming what serve --services does not take is refused.
	doc = unitDoc()
	doc.Profiles[1].Units[0].Services = []string{"nothing"}
	if _, refusals := infra.CheckUnits(doc, unitServices()); len(refusals) < 1 || refusals[0].Resource != "profile:data" || !strings.Contains(refusals[0].Reason, "nothing") {
		t.Fatalf("unknown service: %+v", refusals)
	}
	// all serves everything: nothing to refuse.
	doc = unitDoc()
	doc.Profiles[0].Units[0].Services = []string{"all"}
	if a, refusals := infra.CheckUnits(doc, unitServices()); len(refusals) != 0 || len(a["front"][0].Services) != 4 {
		t.Fatalf("all: %+v %+v", a, refusals)
	}
}
