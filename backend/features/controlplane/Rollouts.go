package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/packages"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/nats-io/nats.go/jetstream"
)

// Channels, rollouts and rollback (forge 20f-2). A package pin or unit
// that names a channel and no version resolves, per instance, to the
// version its channel's rollout has reached: a channel moving does not
// move every instance at once. A rollout plans waves of the instances
// following the channel and moves one wave at a time, past a health gate
// (the version installed, the daemon beating, every unit active), and
// stops on the first failure or a wave's timeout. Its state is its record,
// advanced by every process's sweep with expected-sequence updates, so any
// process continues a rollout another began and no step is done twice.
// A pinned version rolls an instance back.

// Rollout statuses.
const (
	RolloutStarting = "starting"
	RolloutRunning  = "running"
	RolloutPaused   = "paused"
	RolloutDone     = "done"
	RolloutFailed   = "failed"
	RolloutAborted  = "aborted"
)

// DefaultWaveTimeout is how long a wave may take when its rollout says not.
var DefaultWaveTimeout = 10 * time.Minute

func versionsOf(r map[string]json.RawMessage, field string) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(r[field], &m)
	return m
}

func numOf(r map[string]json.RawMessage, field string) int {
	var f float64
	if json.Unmarshal(r[field], &f) != nil {
		var s string
		if json.Unmarshal(r[field], &s) == nil {
			f, _ = strconv.ParseFloat(s, 64)
		}
	}
	return int(f)
}

func wavesOf(r map[string]json.RawMessage) [][]string {
	var w [][]string
	_ = json.Unmarshal(r["waves"], &w)
	return w
}

// channelNamed is the channel record of that name.
func channelNamed(read map[string]map[string]map[string]json.RawMessage, name string) map[string]json.RawMessage {
	for _, c := range read[EntityChannel] {
		if str(c, "name") == name {
			return c
		}
	}
	return nil
}

// latestRollout is the newest rollout of a channel's package to version to.
func latestRollout(read map[string]map[string]map[string]json.RawMessage, channel, pkg, to string) map[string]json.RawMessage {
	var best map[string]json.RawMessage
	for _, r := range read[EntityRollout] {
		if str(r, "channel") != channel || str(r, "package") != pkg || str(r, "to") != to {
			continue
		}
		if best == nil || string(r[data.MetaCreatedAt]) > string(best[data.MetaCreatedAt]) ||
			(string(r[data.MetaCreatedAt]) == string(best[data.MetaCreatedAt]) && string(r[data.MetaID]) > string(best[data.MetaID])) {
			best = r
		}
	}
	return best
}

// resolveVersion is the version of pkg an instance following channel
// installs: with a rollout to the channel's version, that version once
// the rollout is done or reached the instance's wave, else the version it
// leaves; with none, the channel's previous version while there is one,
// else its version.
func resolveVersion(read map[string]map[string]map[string]json.RawMessage, channel, pkg, instance string) string {
	ch := channelNamed(read, channel)
	if ch == nil {
		return ""
	}
	cur, prev := versionsOf(ch, "versions")[pkg], versionsOf(ch, "previous")[pkg]
	r := latestRollout(read, channel, pkg, cur)
	if r == nil || len(wavesOf(r)) == 0 && str(r, "status") != RolloutDone {
		if prev != "" {
			return prev
		}
		return cur
	}
	if str(r, "status") == RolloutDone {
		return cur
	}
	waves, at := wavesOf(r), numOf(r, "wave")
	for i := 0; i <= at && i < len(waves); i++ {
		if contains(waves[i], instance) {
			return cur
		}
	}
	return str(r, "from")
}

// following lists the instances with a pin or unit of pkg that names
// channel and no version.
func following(read map[string]map[string]map[string]json.RawMessage, channel, pkg string) []string {
	var out []string
	for id, in := range read[EntityInstance] {
		ok := false
		for _, p := range read[EntityPackagePin] {
			if t := str(p, "target"); (t == id || t != "" && t == str(in, "profile")) && str(p, "name") == pkg && str(p, "channel") == channel && str(p, "version") == "" {
				ok = true
			}
		}
		for _, u := range read[EntityUnit] {
			if str(u, "instance") == id && str(u, "package") == pkg && str(u, "channel") == channel && str(u, "version") == "" {
				ok = true
			}
		}
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// sweepRollouts advances rollouts in every process, every Sweep.
func sweepRollouts(ctx context.Context, deps func() Deps) {
	for {
		d := deps()
		every := d.Sweep
		if every == 0 {
			every = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		if err := AdvanceRollouts(ctx, d); err != nil && ctx.Err() == nil {
			log.Error(ctx, err, "controlplane: cannot advance rollouts")
		}
	}
}

// AdvanceRollouts takes every starting or running rollout one step, if it
// can: a starting one gets its waves planned; a running one moves to its
// next wave when every instance of this one is healthy on the new version,
// and fails when one is unhealthy on it or the wave times out.
func AdvanceRollouts(ctx context.Context, d Deps) error {
	read := map[string]map[string]map[string]json.RawMessage{}
	for _, e := range []string{EntityRollout, EntityChannel, EntityInstance, EntityUnit, EntityPackagePin} {
		rs, err := all(ctx, d, e)
		if err != nil {
			return err
		}
		read[e] = rs
	}
	ids := make([]string, 0, len(read[EntityRollout]))
	for id := range read[EntityRollout] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := read[EntityRollout][id]
		var set map[string]any
		switch str(r, "status") {
		case "", RolloutStarting:
			set = planRollout(read, r)
		case RolloutRunning:
			set = checkWave(ctx, d, r)
		}
		if set == nil {
			continue
		}
		if err := setRollout(ctx, d, id, r, set); err != nil {
			return err
		}
	}
	return nil
}

func planRollout(read map[string]map[string]map[string]json.RawMessage, r map[string]json.RawMessage) map[string]any {
	channel, pkg := str(r, "channel"), str(r, "package")
	ch := channelNamed(read, channel)
	to := versionsOf(ch, "versions")[pkg]
	if ch == nil || to == "" {
		return map[string]any{"status": RolloutFailed, "failed": fmt.Sprintf("channel %s has no version of %s", channel, pkg)}
	}
	from := versionsOf(ch, "previous")[pkg]
	insts := following(read, channel, pkg)
	var order []string
	_ = json.Unmarshal(r["order"], &order)
	var ordered []string
	seen := map[string]bool{}
	for _, id := range order {
		if contains(insts, id) && !seen[id] {
			seen[id] = true
			ordered = append(ordered, id)
		}
	}
	for _, id := range insts {
		if !seen[id] {
			ordered = append(ordered, id)
		}
	}
	size := numOf(r, "waveSize")
	if size < 1 {
		size = 1
	}
	waves := [][]string{}
	for i := 0; i < len(ordered); i += size {
		waves = append(waves, ordered[i:min(i+size, len(ordered))])
	}
	status := RolloutRunning
	if len(waves) == 0 {
		status = RolloutDone
	}
	return map[string]any{"from": from, "to": to, "waves": waves, "wave": 0, "waveStartedAt": time.Now().UTC().Format(time.RFC3339), "status": status, "failed": ""}
}

// agentFacts are what an agent reports that a rollout reads.
type agentFacts struct {
	Packages map[string]string `json:"packages"`
	Units    map[string]string `json:"units"`
}

func checkWave(ctx context.Context, d Deps, r map[string]json.RawMessage) map[string]any {
	waves, at := wavesOf(r), numOf(r, "wave")
	if at >= len(waves) {
		return map[string]any{"status": RolloutDone}
	}
	pkg, to := str(r, "package"), str(r, "to")
	var pending []string
	for _, inst := range waves[at] {
		t := ctl.Target{Kind: ctl.KindInstance, ID: inst}
		raw, _, err := d.Control.Facts(ctx, t)
		if err != nil {
			return nil
		}
		var f agentFacts
		_ = json.Unmarshal(raw, &f)
		if f.Packages[pkg] != to {
			pending = append(pending, inst)
			continue
		}
		if _, up, _ := d.Control.Health(ctx, t); !up {
			return map[string]any{"status": RolloutFailed, "failed": inst + ": its daemon stopped beating on " + to}
		}
		for name, state := range f.Units {
			if state != "active" {
				return map[string]any{"status": RolloutFailed, "failed": inst + ": unit " + name + " is " + state + " on " + to}
			}
		}
	}
	if len(pending) > 0 {
		started, _ := time.Parse(time.RFC3339, str(r, "waveStartedAt"))
		timeout := DefaultWaveTimeout
		if s := numOf(r, "waveTimeoutSeconds"); s > 0 {
			timeout = time.Duration(s) * time.Second
		}
		if !started.IsZero() && time.Since(started) > timeout {
			return map[string]any{"status": RolloutFailed, "failed": pending[0] + ": did not report " + pkg + " " + to + " within " + timeout.String()}
		}
		return nil
	}
	if at+1 >= len(waves) {
		return map[string]any{"status": RolloutDone}
	}
	return map[string]any{"wave": at + 1, "waveStartedAt": time.Now().UTC().Format(time.RFC3339)}
}

// setRollout updates a rollout at the sequence it was read at: a process
// that took the step first makes this one's update stale, and the same
// step from two processes has one command id.
func setRollout(ctx context.Context, d Deps, id string, r map[string]json.RawMessage, set map[string]any) error {
	changes := map[string]json.RawMessage{}
	for k, v := range set {
		raw, _ := json.Marshal(v)
		changes[k] = raw
	}
	seq := string(r[data.MetaSeq])
	var expect json.RawMessage = r[data.MetaSeq]
	a, _ := json.Marshal(map[string]any{"entity": EntityRollout, "set": changes, "expect": expect})
	args := rootArgs(ctx)
	res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: "ctl-rollout-" + id + "-" + seq, Type: "record.update", Target: id, Args: a, Actor: "controlplane"})
	if res.Common.Code >= 300 && res.Common.ErrorCode != "record_changed" && res.Common.ErrorCode != "record_not_found" {
		return log.Code("rollout_refused").Errorf("cannot advance rollout %s: %s", id, res.Common.Message)
	}
	if res.Common.Code < 300 {
		log.With("rollout", id).Info(ctx, "controlplane: rollout %s: %v", id, set)
	}
	return nil
}

// SyncChannels keeps the channel records as a published repository says
// (channel → package → version): a changed version keeps the one before
// as previous, where instances stay until a rollout moves them. gen makes
// the commands idempotent across processes.
func SyncChannels(ctx context.Context, d Deps, gen string, channels map[string]map[string]string) error {
	if err := d.Projections.Settle(ctx, features.RootTenantID, "records.record"); err != nil {
		return log.Wrap(err)
	}
	rs, err := all(ctx, d, EntityChannel)
	if err != nil {
		return err
	}
	byName := map[string]string{}
	for id, r := range rs {
		byName[str(r, "name")] = id
	}
	names := make([]string, 0, len(channels))
	for n := range channels {
		names = append(names, n)
	}
	sort.Strings(names)
	args := rootArgs(ctx)
	for _, name := range names {
		want := channels[name]
		id, ok := byName[name]
		var req bus.Request
		if !ok {
			a, _ := json.Marshal(map[string]any{"entity": EntityChannel, "values": map[string]any{"name": name, "versions": want}})
			req = bus.Request{ServiceArgs: args, ID: "ctl-channel-" + gen + "-" + name, Type: "record.create", Args: a, Actor: "controlplane"}
		} else {
			old := versionsOf(rs[id], "versions")
			if same(old, want) {
				continue
			}
			prev := versionsOf(rs[id], "previous")
			for pkg, v := range want {
				if o := old[pkg]; o != "" && o != v {
					prev[pkg] = o
				}
			}
			a, _ := json.Marshal(map[string]any{"entity": EntityChannel, "set": map[string]any{"versions": want, "previous": prev}})
			req = bus.Request{ServiceArgs: args, ID: "ctl-channel-" + gen + "-" + name, Type: "record.update", Target: id, Args: a, Actor: "controlplane"}
		}
		if res := d.Bus.Execute(req); res.Common.Code >= 300 {
			return log.Code("channel_refused").Errorf("cannot keep channel %s: %s", name, res.Common.Message)
		}
	}
	return nil
}

func same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// watchRepo syncs the channels each time a repository is published
// (packages.Current changes in ctl_repo).
func watchRepo(ctx context.Context, deps func() Deps) {
	for ctx.Err() == nil {
		err := func() error {
			store, err := packages.Store(ctx, deps().Control.JS)
			if err != nil {
				return err
			}
			w, err := store.Watch(ctx, jetstream.UpdatesOnly())
			if err != nil {
				return log.Wrap(err)
			}
			defer w.Stop()
			sync := func() {
				gen, channels, err := packages.Channels(ctx, store)
				if err == nil && gen != "" {
					err = SyncChannels(ctx, deps(), gen, channels)
				}
				if err != nil {
					log.Error(ctx, err, "controlplane: cannot sync channels from the repository")
				}
			}
			sync()
			for info := range w.Updates() {
				if info != nil && info.Name == packages.Current && !info.Deleted {
					sync()
				}
			}
			return nil
		}()
		if err != nil && ctx.Err() == nil {
			log.Error(ctx, err, "controlplane: cannot watch the repository")
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// resolveChannel sets a pin's or unit's version from its channel when it
// names one and no version: the version its instance's rollout reached.
func resolveChannel(read map[string]map[string]map[string]json.RawMessage, doc map[string]json.RawMessage, pkg, instance string) {
	if pkg == "" || str(doc, "version") != "" || str(doc, "channel") == "" {
		return
	}
	if v := resolveVersion(read, str(doc, "channel"), pkg, instance); v != "" {
		doc["version"], _ = json.Marshal(v)
	}
}
