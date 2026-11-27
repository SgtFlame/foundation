package infra_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
)

// Three sites: two in the cloud joined by a gateway, one on premise
// joined to the first cloud site as a leaf (forge 22h).
func TestNatsTopology(t *testing.T) {
	doc := infra.Document{
		Accounts: []infra.Account{{ID: "test", Name: "Test", Provider: "infratest", Connection: "c1"}},
		Sites: []infra.Site{{ID: "east", Name: "East", Account: "test", Region: "r1"}, {ID: "west", Name: "West", Account: "test", Region: "r2"},
			{ID: "lab", Name: "Lab", OnPremise: true}},
		Clusters: []infra.Cluster{{ID: "east", Site: "east"}, {ID: "west", Site: "west"}, {ID: "lab", Site: "lab", NatsReplicas: 1}},
		HostPools: []infra.HostPool{{ID: "g", Site: "east", Size: "m", Count: 4}, {ID: "g", Site: "west", Size: "m", Count: 2},
			{ID: "g", Site: "lab", Size: "m", Count: 2}},
	}
	rs, _ := infra.Expand(kindsReg(), doc)
	var ids []string
	byID := map[string]infra.Resource{}
	for _, r := range rs {
		if r.Kind == infra.KindNatsNode || r.Kind == infra.KindGateway {
			ids = append(ids, r.ID)
			byID[r.ID] = r
		}
	}
	want := "natsNode:east/1 natsNode:east/2 natsNode:east/3 natsNode:lab/1 natsNode:west/1 natsNode:west/2 gateway:east~lab gateway:east~west"
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("resources = %s", got)
	}
	// Three nodes on three different hosts, never more than the hosts.
	hosts := map[any]bool{}
	for n := 1; n <= 3; n++ {
		hosts[byID[infra.NatsNodeID("east", n)].Props["host"]] = true
	}
	if len(hosts) != 3 {
		t.Fatalf("east's nodes are on %v", hosts)
	}
	e1 := byID["natsNode:east/1"].Props
	if fmt.Sprint(e1["peers"]) != "[natsNode_east_1__nats natsNode_east_2__nats natsNode_east_3__nats]" ||
		fmt.Sprint(e1["gateways"]) != "[map[name:west urls:[nats://nats-west-1:7222 nats://nats-west-2:7222]]]" || fmt.Sprint(e1["leafRemotes"]) != "[]" {
		t.Fatalf("east's cluster = %v", e1)
	}
	l1 := byID["natsNode:lab/1"].Props
	if fmt.Sprint(l1["gateways"]) != "[]" || !strings.Contains(fmt.Sprint(l1["leafRemotes"]), "hub:east") {
		t.Fatalf("lab's cluster = %v", l1)
	}
	if g := byID["gateway:east~lab"]; g.Props["mode"] != "leaf" || len(g.DependsOn) != 4 {
		t.Fatalf("the leaf = %+v", g)
	}
	if g := byID["gateway:east~west"]; g.Props["mode"] != "gateway" {
		t.Fatalf("the gateway = %+v", g)
	}
	// A site without a cluster has no NATS nodes.
	doc.Clusters = doc.Clusters[:1]
	rs, _ = infra.Expand(kindsReg(), doc)
	n := 0
	for _, r := range rs {
		if r.Kind == infra.KindNatsNode {
			n++
		}
		if r.Kind == infra.KindGateway {
			t.Fatalf("a gateway with one cluster: %+v", r)
		}
	}
	if n != 3 {
		t.Fatalf("%d NATS nodes", n)
	}
}
