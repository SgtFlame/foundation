package controlplane

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/nats-io/nats.go/jetstream"
)

// Deps are what the followers work with: the root tenant's records (read
// through data, changed through the bus) and the control account.
type Deps struct {
	Projections *project.Projections
	Data        data.Deps
	Bus         *bus.Bus
	Control     *ctl.Control
	// Sweep is how often health is checked (2 s when 0).
	Sweep time.Duration
	// ScriptKey signs infraScript records' content (forge 20c-3); its
	// public half is every target's trusted scriptKeys. Nil signs nothing.
	ScriptKey ed25519.PrivateKey
}

// reported are the fields daemons fill: never part of desired state.
var reported = map[string]bool{"facts": true, "health": true, "lastEvent": true, "capabilities": true, "hash": true, "problem": true}

// Start runs the control plane's followers in this process (every process
// runs them; each is idempotent): controlplane.desired copies each host's
// and instance's resolved desired state into ctl_desired and nudges its
// daemon with a reconcile request when it changed; controlplane.actual
// folds CTL's events, facts and heartbeats into the records' reported
// fields.
func Start(ctx context.Context, deps func() Deps) error {
	d := deps()
	if err := d.Control.Ensure(ctx); err != nil {
		return err
	}
	var subjects []string
	for _, e := range []string{EntityHost, EntityProfile, EntityInstance, EntityUnit, EntityPackagePin, EntityAptSource} {
		subjects = append(subjects, "evt."+strconv.FormatUint(features.RootTenantID, 10)+".rec."+e+".>")
	}
	err := d.Projections.Follow(ctx, features.RootTenantID, live.AreaRecords, "controlplane.desired", subjects, func(e live.Event) error {
		return Resolve(ctx, deps(), e.Actor)
	})
	if err != nil {
		return log.Wrap(err)
	}
	err = d.Projections.Follow(ctx, features.RootTenantID, live.AreaRecords, "controlplane.scripts",
		[]string{"evt." + strconv.FormatUint(features.RootTenantID, 10) + ".rec." + EntityScript + ".>"}, func(live.Event) error {
			return SignScripts(ctx, deps())
		})
	if err != nil {
		return log.Wrap(err)
	}
	if err := followEvents(ctx, deps); err != nil {
		return err
	}
	go watchFacts(ctx, deps)
	go sweepHealth(ctx, deps)
	return nil
}

func rootArgs(ctx context.Context) features.ServiceArgs {
	return RootArgs(features.ServiceArgs{Ctx: ctx})
}

// all reads every record of an entity type, without its bookkeeping.
func all(ctx context.Context, d Deps, entity string) (map[string]map[string]json.RawMessage, error) {
	res, err := data.Find(rootArgs(ctx), d.Data, data.Query{Entity: entity})
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]json.RawMessage{}
	for _, r := range res.Records {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		out[id] = r
	}
	return out, nil
}

// desired is a record's desired fields: its own data without bookkeeping
// or reported fields.
func desired(r map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range r {
		if strings.HasPrefix(k, "_") || reported[k] {
			continue
		}
		out[k] = v
	}
	if id, ok := r[data.MetaID]; ok {
		out["id"] = id
	}
	return out
}

func str(r map[string]json.RawMessage, field string) string {
	var s string
	_ = json.Unmarshal(r[field], &s)
	return s
}

// Resolve copies every host's and instance's resolved desired state into
// ctl_desired, removes those whose record is gone, and nudges the daemon
// of each that changed (actor: who changed it). An instance is resolved
// with its profile, its units and the package pins on it or its profile.
func Resolve(ctx context.Context, d Deps, actor string) error {
	if err := d.Projections.Settle(ctx, features.RootTenantID, "records.record"); err != nil {
		return log.Wrap(err)
	}
	read := map[string]map[string]map[string]json.RawMessage{}
	for _, e := range []string{EntityHost, EntityProfile, EntityInstance, EntityUnit, EntityPackagePin, EntityAptSource} {
		rs, err := all(ctx, d, e)
		if err != nil {
			return err
		}
		read[e] = rs
	}
	docs := map[ctl.Target]any{}
	for id, h := range read[EntityHost] {
		doc := desired(h)
		var instances []map[string]json.RawMessage
		profiles := map[string]map[string]json.RawMessage{}
		for iid, in := range read[EntityInstance] {
			if str(in, "host") == id {
				i := desired(in)
				i["id"], _ = json.Marshal(iid)
				instances = append(instances, i)
				if pid := str(in, "profile"); read[EntityProfile][pid] != nil {
					profiles[pid] = desired(read[EntityProfile][pid])
				}
			}
		}
		sortByID(instances)
		docs[ctl.Target{Kind: ctl.KindHost, ID: id}] = map[string]any{"host": doc, "instances": instances, "profiles": profiles}
	}
	for id, in := range read[EntityInstance] {
		doc := desired(in)
		prof := read[EntityProfile][str(in, "profile")]
		var profile map[string]json.RawMessage
		if prof != nil {
			profile = desired(prof)
			if str(in, "kind") == "" {
				doc["kind"] = prof["kind"]
			}
		}
		var units, pins []map[string]json.RawMessage
		for _, u := range read[EntityUnit] {
			if str(u, "instance") == id {
				units = append(units, desired(u))
			}
		}
		for _, p := range read[EntityPackagePin] {
			if t := str(p, "target"); t == id || (t != "" && t == str(in, "profile")) {
				pins = append(pins, desired(p))
			}
		}
		// The third-party sources its pins name, with whether each is
		// approved (forge 20d-1): the agent writes only approved ones
		var sources []map[string]json.RawMessage
		seen := map[string]bool{}
		for _, p := range pins {
			if sid := str(p, "repository"); sid != "" && !seen[sid] && read[EntityAptSource][sid] != nil {
				seen[sid] = true
				sources = append(sources, desired(read[EntityAptSource][sid]))
			}
		}
		sortByID(units)
		sortByID(pins)
		sortByID(sources)
		docs[ctl.Target{Kind: ctl.KindInstance, ID: id}] = map[string]any{"instance": doc, "profile": profile, "units": units, "pins": pins, "sources": sources}
	}
	for t, doc := range docs {
		if d.ScriptKey != nil {
			doc.(map[string]any)["scriptKeys"] = []string{ctl.PublicKey(d.ScriptKey)}
		}
		raw, _ := json.Marshal(doc)
		changed, err := d.Control.PutDesired(ctx, t, raw)
		if err != nil {
			return err
		}
		if changed {
			nudge(ctx, d, t, actor)
		}
	}
	// Targets whose record is gone
	kv, err := d.Control.KV(ctx, ctl.DesiredBucket)
	if err != nil {
		return err
	}
	keys, err := kv.ListKeys(ctx)
	if err != nil {
		return log.Wrap(err)
	}
	for k := range keys.Keys() {
		if t, ok := ctl.TargetOfKey(k); ok {
			if _, ok := docs[t]; !ok {
				if err := d.Control.DeleteDesired(ctx, t); err != nil {
					return err
				}
				nudge(ctx, d, t, actor)
			}
		}
	}
	return nil
}

func sortByID(rs []map[string]json.RawMessage) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && string(rs[j]["id"]) < string(rs[j-1]["id"]); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// nudge asks a target's daemon to reconcile; a daemon not running now
// reconciles when it starts, so no answer is not a failure.
func nudge(ctx context.Context, d Deps, t ctl.Target, actor string) {
	if actor == "" {
		actor = "controlplane"
	}
	r := ctl.Request{ID: "reconcile-" + t.Key() + "-" + strconv.FormatUint(features.NewTSID(), 10), Verb: "reconcile", Actor: actor}
	if _, err := d.Control.Send(ctx, t, r); err != nil && !errors.Is(err, ctl.ErrNoDaemon) {
		log.With("target", t.Key()).Debug(ctx, "controlplane: no answer to reconcile: %v", err)
	}
}

func entityOf(t ctl.Target) string {
	switch t.Kind {
	case ctl.KindHost:
		return EntityHost
	case "script":
		return EntityScript
	}
	return EntityInstance
}

// update sets reported fields on a target's record, if it still exists
// and they differ; id makes the command idempotent.
func update(ctx context.Context, d Deps, t ctl.Target, id string, set map[string]any) error {
	args := rootArgs(ctx)
	r, _, err := data.Get(args, d.Data, entityOf(t), t.ID)
	if err != nil || r == nil {
		return nil // the record is gone, or not readable yet
	}
	changes := map[string]json.RawMessage{}
	for k, v := range set {
		raw, _ := json.Marshal(v)
		if string(r[k]) != string(raw) {
			changes[k] = raw
		}
	}
	if len(changes) == 0 {
		return nil
	}
	a, _ := json.Marshal(map[string]any{"entity": entityOf(t), "set": changes})
	res := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: id, Type: "record.update", Target: t.ID, Args: a, Actor: "controlplane"})
	if res.Common.Code >= 300 && res.Common.ErrorCode != "record_not_found" {
		return log.Code("report_refused").Errorf("cannot report on %s: %s", t.Key(), res.Common.Message)
	}
	return nil
}

// followEvents folds CTL's events into the targets' lastEvent, through
// one durable consumer shared by every process.
func followEvents(ctx context.Context, deps func() Deps) error {
	d := deps()
	cons, err := d.Control.JS.CreateOrUpdateConsumer(ctx, ctl.StreamName, jetstream.ConsumerConfig{
		Durable: "ctl_actual", AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy, AckWait: 30 * time.Second,
	})
	if err != nil {
		return log.Wrap(err)
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var e ctl.Event
		if json.Unmarshal(m.Data(), &e) != nil {
			_ = m.Term()
			return
		}
		md, _ := m.Metadata()
		id := "ctl-event-" + strconv.FormatUint(md.Sequence.Stream, 10)
		last := map[string]any{"request": e.Request, "kind": e.Kind, "verb": e.Verb, "message": e.Message, "at": e.At}
		if err := update(ctx, deps(), e.Target, id, map[string]any{"lastEvent": last}); err != nil {
			log.Error(ctx, err, "controlplane: cannot fold a CTL event")
			_ = m.NakWithDelay(time.Second)
			return
		}
		_ = m.Ack()
	})
	if err != nil {
		return log.Wrap(err)
	}
	go func() { <-ctx.Done(); cc.Stop() }()
	return nil
}

// watchFacts copies ctl_facts into the records' facts.
func watchFacts(ctx context.Context, deps func() Deps) {
	for ctx.Err() == nil {
		kv, err := deps().Control.KV(ctx, ctl.FactsBucket)
		if err == nil {
			var w jetstream.KeyWatcher
			if w, err = kv.WatchAll(ctx); err == nil {
				for e := range w.Updates() {
					if e == nil || e.Operation() != jetstream.KeyValuePut {
						continue
					}
					t, ok := ctl.TargetOfKey(e.Key())
					if !ok {
						continue
					}
					var facts map[string]any
					_ = json.Unmarshal(e.Value(), &facts)
					set := map[string]any{"facts": facts}
					if c, ok := facts["capabilities"]; ok && t.Kind == ctl.KindHost {
						set["capabilities"] = c // a host's, from its facts (forge 20c-2)
					}
					id := "ctl-facts-" + t.Key() + "-" + strconv.FormatUint(e.Revision(), 10)
					for try := 0; try < 10; try++ {
						if err := update(ctx, deps(), t, id, set); err == nil {
							break
						}
						time.Sleep(time.Second)
					}
				}
				_ = w.Stop()
			}
		}
		if err != nil {
			log.Error(ctx, err, "controlplane: cannot watch facts")
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// sweepHealth marks hosts and instances up while their heartbeat is
// fresh and down once it is not (unknown until the first).
func sweepHealth(ctx context.Context, deps func() Deps) {
	every := deps().Sweep
	if every == 0 {
		every = 2 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		d := deps()
		for kind, entity := range map[string]string{ctl.KindHost: EntityHost, ctl.KindInstance: EntityInstance} {
			rs, err := all(ctx, d, entity)
			if err != nil {
				continue
			}
			for id, r := range rs {
				target := ctl.Target{Kind: kind, ID: id}
				_, up, err := d.Control.Health(ctx, target)
				if err != nil {
					continue
				}
				was := str(r, "health")
				now := "down"
				if up {
					now = "up"
				} else if was == "" || was == "unknown" {
					continue
				}
				if now != was {
					_ = update(ctx, d, target, "ctl-health-"+strconv.FormatUint(features.NewTSID(), 10), map[string]any{"health": now})
				}
			}
		}
	}
}

// SignScripts signs and keeps every infraScript record's content whose
// hash is not yet reported (forge 20c-3), reporting its hash, or the
// problem that stopped it. Idempotent: a script is kept by its hash.
func SignScripts(ctx context.Context, d Deps) error {
	if d.ScriptKey == nil {
		return nil
	}
	if err := d.Projections.Settle(ctx, features.RootTenantID, "records.record"); err != nil {
		return log.Wrap(err)
	}
	scripts, err := all(ctx, d, EntityScript)
	if err != nil {
		return err
	}
	t := ctl.Target{Kind: "script"}
	for id, r := range scripts {
		content := str(r, "content")
		hash := ctl.HashOf([]byte(content))
		if str(r, "hash") == hash && str(r, "problem") == "" {
			continue
		}
		set := map[string]any{"hash": hash, "problem": ""}
		if err := ctl.CheckScript(content); err != nil {
			set = map[string]any{"hash": "", "problem": err.Error()}
		} else if _, err := d.Control.PutScript(ctx, d.ScriptKey, content); err != nil {
			return err
		}
		if str(r, "hash") == set["hash"] && str(r, "problem") == set["problem"] {
			continue
		}
		t.ID = id
		if err := update(ctx, d, t, "ctl-script-"+id+"-"+hash, set); err != nil {
			return err
		}
	}
	return nil
}
