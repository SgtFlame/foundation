package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/samber/oops"
	"github.com/nats-io/nats.go/jetstream"
)

// TestNatsRollout: three agents run a cluster's three NATS nodes; a
// changed route reloads each node, a new gateway restarts each, never two
// at once; a renamed cluster is refused unless allowed; a lease someone
// else holds makes an agent wait, and the change is applied once it is
// free.
func TestNatsRollout(t *testing.T) {
	ctx := context.Background()
	s := natsauth.StartTestSecure(t)
	cp, _ := s.Conn(natsauth.Control, "serve", natsauth.Everything)
	defer cp.Close()
	cpJS, _ := jetstream.New(cp)
	control := &ctl.Control{NC: cp, JS: cpJS}
	if err := control.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	var busy, most atomic.Int32
	ready := func(context.Context, Unit) error {
		n := busy.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		busy.Add(-1)
		return nil
	}
	hosts := []string{"n1", "n2", "n3"}
	fakes := make([]*fake, 3)
	for i, h := range hosts {
		inst := ctl.Target{Kind: ctl.KindInstance, ID: h}
		nc, err := s.Conn(natsauth.Control, "axiomd-inst."+h, ctl.DaemonPermissions(inst))
		if err != nil {
			t.Fatal(err)
		}
		defer nc.Close()
		js, _ := jetstream.New(nc)
		c := &ctl.Control{NC: nc, JS: js}
		f := newFake(t)
		f.a.Control, f.a.Instance, f.a.NatsReady = func() *ctl.Control { return c }, h, ready
		fakes[i] = f
	}
	trust := s.D.Trust()
	node := func(i int, cluster string, routes []string, gateways []NatsGateway, allow bool) NatsNode {
		var rs []string
		for j, h := range hosts {
			if j != i {
				rs = append(rs, "nats-route://"+h+":6222")
			}
		}
		return NatsNode{Cluster: cluster, ServerName: hosts[i] + "-nats", Host: hosts[i], Routes: append(rs, routes...),
			Ports: map[string]int{"client": 4222, "cluster": 6222, "gateway": 7222, "leaf": 7422, "monitor": 8222}, Gateways: gateways, AllowFullRestart: allow, Trust: trust}
	}
	doc := func(n NatsNode) map[string]any {
		return map[string]any{"units": []map[string]any{{"name": "nats", "kind": "nats", "nats": n}}}
	}
	all := func(nodeOf func(i int) NatsNode) []error {
		errs := make([]error, 3)
		var wg sync.WaitGroup
		for i, f := range fakes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = f.reconcile(doc(nodeOf(i)))
			}()
		}
		wg.Wait()
		return errs
	}
	ok := func(errs []error) {
		t.Helper()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("node %d: %v", i, err)
			}
		}
	}
	counts := func(prefix string) []int {
		out := make([]int, 3)
		for i, f := range fakes {
			out[i] = f.count(prefix)
		}
		return out
	}
	conf := func(i int) string {
		raw, _ := os.ReadFile(filepath.Join(fakes[i].a.Root, "etc/nats/nats.conf"))
		return string(raw)
	}

	// First start: each node enabled from the template, nothing to roll
	ok(all(func(i int) NatsNode { return node(i, "east", nil, nil, false) }))
	if c := counts("systemctl enable --now axiom-nats@nats"); fmt.Sprint(c) != "[1 1 1]" || most.Load() != 0 {
		t.Fatalf("enabled %v, rolled %d", c, most.Load())
	}
	c0 := conf(0)
	for _, want := range []string{`server_name: "n1-nats"`, `name: "east"`, `"nats-route://n2:6222"`, `"nats-route://n3:6222"`, "resolver_preload", `http: "127.0.0.1:8222"`} {
		if !strings.Contains(c0, want) {
			t.Fatalf("no %s in\n%s", want, c0)
		}
	}
	tmpl, _ := os.ReadFile(filepath.Join(fakes[0].a.Root, "etc/systemd/system/axiom-nats@.service"))
	if !strings.Contains(string(tmpl), "ExecReload=/bin/kill -HUP $MAINPID") {
		t.Fatalf("template\n%s", tmpl)
	}
	// The rendered file is a configuration nats-server accepts
	path := filepath.Join(t.TempDir(), "nats.conf")
	_ = os.WriteFile(path, []byte(strings.ReplaceAll(c0, "/var/lib/axiom", t.TempDir())), 0o600)
	if _, err := server.ProcessConfigFile(path); err != nil {
		t.Fatalf("nats-server refuses the configuration: %v\n%s", err, c0)
	}

	// A route added: a reload on each node, one at a time
	ok(all(func(i int) NatsNode { return node(i, "east", []string{"nats-route://n4:6222"}, nil, false) }))
	if c := counts("systemctl reload axiom-nats@nats"); fmt.Sprint(c) != "[1 1 1]" || fmt.Sprint(counts("systemctl restart")) != "[0 0 0]" {
		t.Fatalf("reloads %v, restarts %v", c, counts("systemctl restart"))
	}
	if most.Load() != 1 {
		t.Fatalf("%d nodes changed at once", most.Load())
	}
	kv, _ := cpJS.KeyValue(ctx, ctl.RolloutBucket)
	if _, err := kv.Get(ctx, "east"); !errors.Is(err, jetstream.ErrKeyNotFound) {
		t.Fatalf("the lease was kept: %v", err)
	}
	// A gateway added: a restart on each node, one at a time
	gw := []NatsGateway{{Name: "west", URLs: []string{"nats://w1:7222"}}}
	ok(all(func(i int) NatsNode { return node(i, "east", []string{"nats-route://n4:6222"}, gw, false) }))
	if c := counts("systemctl restart axiom-nats@nats"); fmt.Sprint(c) != "[1 1 1]" || most.Load() != 1 {
		t.Fatalf("restarts %v, at once %d", c, most.Load())
	}
	if !strings.Contains(conf(1), `{ name: "west", urls: ["nats://w1:7222"] }`) {
		t.Fatalf("gateway\n%s", conf(1))
	}

	// A renamed cluster is refused, and the file kept, unless allowed
	errs := all(func(i int) NatsNode { return node(i, "east2", []string{"nats-route://n4:6222"}, gw, false) })
	for i, err := range errs {
		if codeOf(err) != "nats_full_restart" {
			t.Fatalf("node %d: %v", i, err)
		}
	}
	if !strings.Contains(conf(0), `name: "east"`) || fmt.Sprint(counts("systemctl restart axiom-nats@nats")) != "[1 1 1]" {
		t.Fatalf("a refused change was applied\n%s", conf(0))
	}
	ok(all(func(i int) NatsNode { return node(i, "east2", []string{"nats-route://n4:6222"}, gw, true) }))
	if c := counts("systemctl restart axiom-nats@nats"); fmt.Sprint(c) != "[2 2 2]" || most.Load() != 1 {
		t.Fatalf("restarts %v, at once %d", c, most.Load())
	}

	// A lease held elsewhere: the agent waits, then gives up; the change
	// stays pending and is applied once the lease is free
	old := RolloutWait
	RolloutWait = 500 * time.Millisecond
	defer func() { RolloutWait = old }()
	rev, err := kv.Create(ctx, "east2", []byte(`{"instance":"someone"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := fakes[0].reconcile(doc(node(0, "east2", nil, gw, true))); codeOf(err) != "nats_rollout_busy" {
		t.Fatalf("with the lease taken: %v", err)
	}
	if fakes[0].count("systemctl reload") != 1 {
		t.Fatalf("reloaded without the lease: %v", fakes[0].calls())
	}
	_ = kv.Delete(ctx, "east2", jetstream.LastRevision(rev))
	if err := fakes[0].reconcile(doc(node(0, "east2", nil, gw, true))); err != nil {
		t.Fatal(err)
	}
	if fakes[0].count("systemctl reload") != 2 {
		t.Fatalf("the pending change was not applied: %v", fakes[0].calls())
	}
}

func codeOf(err error) string {
	if o, ok := oops.AsOops(err); ok {
		return fmt.Sprint(o.Code())
	}
	return ""
}
