package controlplane

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// NATS configuration as desired state (forge 20e): infraNatsCluster
// records become each NATS node's part of its instance's desired state,
// which the agent renders into nats-server.conf and applies one node at a
// time; infraNatsAccount records become tenant accounts, signed and pushed
// through the resolver with no restart.

// Default NATS ports, used where a cluster's ports leave one out.
var natsPorts = map[string]int{"client": 4222, "cluster": 6222, "gateway": 7222, "leaf": 7422, "monitor": 8222}

// natsOf is a nats unit's node configuration from the cluster that names
// it among its nodes, nil when none does.
func natsOf(read map[string]map[string]map[string]json.RawMessage, d Deps, u map[string]json.RawMessage) map[string]any {
	var uid string
	_ = json.Unmarshal(u[data.MetaID], &uid)
	for _, c := range read[EntityNatsCluster] {
		var nodes []string
		_ = json.Unmarshal(c["nodes"], &nodes)
		if !contains(nodes, uid) {
			continue
		}
		ports := map[string]int{}
		_ = json.Unmarshal(c["ports"], &ports)
		for k, v := range natsPorts {
			if ports[k] == 0 {
				ports[k] = v
			}
		}
		var routes []string
		for _, n := range nodes {
			if nu := read[EntityUnit][n]; nu != nil && n != uid {
				routes = append(routes, "nats-route://"+nodeHost(read, nu)+":"+strconv.Itoa(ports["cluster"]))
			}
		}
		sort.Strings(routes)
		host := nodeHost(read, u)
		var full bool
		_ = json.Unmarshal(c["allowFullRestart"], &full)
		out := map[string]any{
			"cluster": str(c, "name"), "domain": str(c, "domain"), "serverName": host + "-" + str(u, "name"), "host": host,
			"routes": routes, "ports": ports, "allowFullRestart": full,
			"gateways": raw(c["gateways"]), "leafRemotes": raw(c["leafRemotes"]),
		}
		if d.NatsTrust != nil {
			if t := d.NatsTrust(); t != nil {
				out["trust"] = t
			}
		}
		return out
	}
	return nil
}

// nodeHost is the name a node is reached at: its config's host, else its
// instance's name (LXD's DNS name).
func nodeHost(read map[string]map[string]map[string]json.RawMessage, u map[string]json.RawMessage) string {
	var c struct {
		Host string `json:"host"`
	}
	_ = json.Unmarshal(u["config"], &c)
	if c.Host != "" {
		return c.Host
	}
	if in := read[EntityInstance][str(u, "instance")]; in != nil {
		return str(in, "name")
	}
	return str(u, "name")
}

func raw(v json.RawMessage) json.RawMessage {
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// followNats applies infraNatsAccount records as they change
// (controlplane.natsAccounts).
func followNats(ctx context.Context, deps func() Deps) error {
	d := deps()
	subjects := []string{"evt." + strconv.FormatUint(features.RootTenantID, 10) + ".rec." + EntityNatsAcct + ".>"}
	err := d.Projections.Follow(ctx, features.RootTenantID, live.AreaRecords, "controlplane.natsAccounts", subjects, func(live.Event) error {
		return ApplyNatsAccounts(ctx, deps())
	})
	return log.Wrap(err)
}

// AccountLimits are an infraNatsAccount record's limits.
type AccountLimits struct {
	Connections int64 `json:"connections"`
	Storage     int64 `json:"storage"`
	Memory      int64 `json:"memory"`
	Streams     int64 `json:"streams"`
}

// ApplyNatsAccounts makes every tenant account match its record: its
// limits signed and pushed (natsauth.ApplyTenant), and its streams moved
// to the record's placement one at a time (natsd.Place) when it names a
// cluster. Every step is idempotent, so any process may run it again.
func ApplyNatsAccounts(ctx context.Context, d Deps) error {
	if d.NatsKeys == nil {
		return nil
	}
	if err := d.Projections.Settle(ctx, features.RootTenantID, "records.record"); err != nil {
		return log.Wrap(err)
	}
	rs, err := all(ctx, d, EntityNatsAcct)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(rs))
	for id := range rs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := rs[id]
		tenant, err := strconv.ParseUint(strings.TrimSpace(str(r, "tenant")), 10, 64)
		if err != nil || tenant == 0 || tenant == features.RootTenantID {
			log.With("record", id).Warn(ctx, "controlplane: NATS account %s names no tenant account: skipped", id)
			continue
		}
		var l AccountLimits
		_ = json.Unmarshal(r["limits"], &l)
		changed, err := natsauth.ApplyTenant(d.NatsKeys, d.NatsSys, tenant, natsauth.Limits(l))
		if err != nil {
			return err
		}
		if changed {
			log.With("tenant", tenant).Info(ctx, "controlplane: applied tenant %d's NATS account", tenant)
		}
		if cluster := strings.TrimSpace(str(r, "placement")); cluster != "" {
			js, err := d.Projections.JetStreamFor(ctx, tenant)
			if err != nil {
				return log.Wrap(err)
			}
			moved, err := natsd.Place(ctx, js, tenant, natsd.Placement{Cluster: cluster})
			if err != nil {
				return err
			}
			if len(moved) > 0 {
				log.With("tenant", tenant).Info(ctx, "controlplane: moved %d of tenant %d's streams to %s", len(moved), tenant, cluster)
			}
		}
	}
	return nil
}

// firstParty is the first-party repository as an approved apt source
// (forge 20f-1): served by `axiomd repo-serve` on every NATS node's
// instance, signed by the repository key, on the profile's channel.
func firstParty(read map[string]map[string]map[string]json.RawMessage, d Deps, profile map[string]json.RawMessage) map[string]json.RawMessage {
	if d.RepoKey == nil {
		return nil
	}
	key := d.RepoKey()
	if key == "" {
		return nil
	}
	seen := map[string]bool{}
	var uris []string
	for _, u := range read[EntityUnit] {
		if str(u, "kind") != "nats" || str(u, "status") == "stopped" {
			continue
		}
		if h := nodeHost(read, u); !seen[h] {
			seen[h] = true
			uris = append(uris, "http://"+h+":"+strconv.Itoa(ctl.RepoPort))
		}
	}
	if len(uris) == 0 {
		return nil
	}
	sort.Strings(uris)
	channel := "stable"
	if c := str(profile, "channel"); c != "" {
		channel = c
	}
	m := func(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
	return map[string]json.RawMessage{
		"id": m("firstParty"), "name": m("first-party"), "uris": m(strings.Join(uris, " ")), "suites": m(channel),
		"components": m("main"), "key": m(key), "approved": m(true),
	}
}
