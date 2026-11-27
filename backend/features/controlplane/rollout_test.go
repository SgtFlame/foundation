package controlplane_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/controlplane/controlplanetest"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/packages"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// TestRollout: six instances following stable stay on the old version
// when the channel moves; a rollout in waves of two stops at the first
// unhealthy instance and leaves the rest where they were; a new rollout
// moves wave by wave, a held wave holding it however many processes
// advance it; a pinned version rolls one instance back.
func TestRollout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newStack(t)
	host := s.create(controlplane.EntityHost, map[string]any{"name": "lxd1", "provider": "lxd"})
	var ids, pins []string
	for i := 0; i < 6; i++ {
		in := s.create(controlplane.EntityInstance, map[string]any{"name": "serve" + string(rune('a'+i)), "host": host})
		ids = append(ids, in)
		pins = append(pins, s.create(controlplane.EntityPackagePin, map[string]any{"target": in, "name": "axiom-server", "source": "firstParty", "channel": "stable"}))
	}
	sort.Strings(ids)
	pinOf := map[string]string{}
	for i := range pins {
		var target string
		// The records projection may not have folded the pin yet
		until(t, "the pin is readable", func() bool { return s.field(controlplane.EntityPackagePin, pins[i], "target") != "" })
		_ = json.Unmarshal([]byte(s.field(controlplane.EntityPackagePin, pins[i], "target")), &target)
		pinOf[target] = pins[i]
	}
	agents := map[string]*controlplanetest.FakeAgent{}
	for _, id := range ids {
		a := &controlplanetest.FakeAgent{C: s.ctl, Target: ctl.Target{Kind: ctl.KindInstance, ID: id}}
		a.Start(ctx)
		agents[id] = a
	}
	versions := func() string {
		var out []string
		for _, id := range ids {
			out = append(out, agents[id].Installed("axiom-server"))
		}
		return strings.Join(out, " ")
	}
	if err := controlplane.SyncChannels(ctx, s.deps, "g1", map[string]map[string]string{"stable": {"axiom-server": "1.0"}}); err != nil {
		t.Fatal(err)
	}
	until(t, "every instance installs 1.0", func() bool { return versions() == "1.0 1.0 1.0 1.0 1.0 1.0" })

	// The channel moves: nobody moves without a rollout
	if err := controlplane.SyncChannels(ctx, s.deps, "g2", map[string]map[string]string{"stable": {"axiom-server": "1.1"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if v := versions(); v != "1.0 1.0 1.0 1.0 1.0 1.0" {
		t.Fatalf("the channel moved instances: %s", v)
	}

	// The first instance fails on the new version: the rollout stops there
	agents[ids[0]].Unhealthy.Store(true)
	r1 := s.create(controlplane.EntityRollout, map[string]any{"channel": "stable", "package": "axiom-server", "waveSize": 2})
	until(t, "the rollout fails", func() bool { return s.field(controlplane.EntityRollout, r1, "status") == `"failed"` })
	if f := s.field(controlplane.EntityRollout, r1, "failed"); !strings.Contains(f, ids[0]) || !strings.Contains(f, "failed") {
		t.Fatalf("failure %s", f)
	}
	time.Sleep(500 * time.Millisecond)
	if v := versions(); v != "1.1 1.1 1.0 1.0 1.0 1.0" {
		t.Fatalf("after the failure: %s", v)
	}

	// Fixed, a new rollout: a held instance holds its wave, whatever
	// processes advance it
	agents[ids[0]].Unhealthy.Store(false)
	agents[ids[2]].Hold.Store(true)
	r2 := s.create(controlplane.EntityRollout, map[string]any{"channel": "stable", "package": "axiom-server", "waveSize": 2})
	until(t, "the second wave starts", func() bool { return s.field(controlplane.EntityRollout, r2, "wave") == "1" })
	until(t, "the second wave's other instance moves", func() bool { return agents[ids[3]].Installed("axiom-server") == "1.1" })
	var wg sync.WaitGroup
	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := controlplane.AdvanceRollouts(ctx, s.deps); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if w, v := s.field(controlplane.EntityRollout, r2, "wave"), versions(); w != "1" || v != "1.1 1.1 1.0 1.1 1.0 1.0" {
		t.Fatalf("held: wave %s, versions %s", w, v)
	}
	agents[ids[2]].Hold.Store(false)
	until(t, "the rollout is done", func() bool { return s.field(controlplane.EntityRollout, r2, "status") == `"done"` })
	until(t, "every instance runs 1.1", func() bool { return versions() == "1.1 1.1 1.1 1.1 1.1 1.1" })

	// Rollback: a pinned version, while the channel stays new
	s.set(controlplane.EntityPackagePin, pinOf[ids[5]], map[string]any{"version": "1.0"})
	until(t, "the pinned instance downgrades", func() bool { return versions() == "1.1 1.1 1.1 1.1 1.1 1.0" })
}

// TestChannelsFromRepository: publishing a repository keeps the channel
// records: made on the first publish, the old version kept as previous on
// the next.
func TestChannelsFromRepository(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	publish := func(version string) {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("Package: axiom-server\nVersion: "+version+"\nArchitecture: amd64\n\n"), 0o644)
		if _, err := packages.Publish(ctx, s.ctl.JS, dir); err != nil {
			t.Fatal(err)
		}
	}
	channel := func() (string, string) {
		rs, _ := data.Find(controlplane.RootArgs(features.ServiceArgs{Ctx: ctx}), s.data, data.Query{Entity: controlplane.EntityChannel})
		if rs.Records == nil || len(rs.Records) != 1 {
			return "", ""
		}
		return string(rs.Records[0]["versions"]), string(rs.Records[0]["previous"])
	}
	publish("1.0")
	until(t, "the channel is made", func() bool { v, _ := channel(); return v == `{"axiom-server":"1.0"}` })
	publish("1.1")
	until(t, "the channel moves, keeping the old version", func() bool {
		v, p := channel()
		return v == `{"axiom-server":"1.1"}` && p == `{"axiom-server":"1.0"}`
	})
}
