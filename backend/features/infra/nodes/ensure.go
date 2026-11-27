package infranodes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// Host is what EnsureHost gives.
type Host struct {
	Record  string `json:"host"`
	State   string `json:"state"`
	Address string `json:"address"`
}

// EnsureHost makes sure a step's host exists at its provider (host.create
// keyed by its provider name) and in the Control Plane (its infraHost
// record). A provider error is returned as it is, so a job can tell a
// retryable one.
func EnsureHost(args features.ServiceArgs, d Deps, s infra.Step, account infra.Account, userData string) (Host, error) {
	if d.Compute == nil {
		return Host{}, fmt.Errorf("there is no compute connector")
	}
	name := str(s.Desired, "name")
	in := map[string]any{"id": name, "size": str(s.Desired, "size"), "region": str(s.Desired, "region")}
	if img := str(s.Desired, "image"); img != "" {
		in["image"] = img
	}
	if userData != "" {
		in["userData"] = userData
	}
	if s.Desired["requiresVms"] == true {
		in["nested"] = true
	}
	raw, err := d.Compute.Call(args, account, "host.create", in)
	if err != nil {
		return Host{}, err
	}
	var h infra.ProviderHost
	_ = json.Unmarshal(raw, &h)
	out := Host{Record: RecordID(s.ID), State: h.State}
	if len(h.Addresses) > 0 {
		out.Address = h.Addresses[0]
	}
	values := map[string]any{"name": name, "site": s.Site, "provider": account.Provider}
	if out.Address != "" {
		values["address"] = out.Address
	}
	if _, err := d.Ensure(args, controlplane.EntityHost, out.Record, values, "infra-"+out.Record); err != nil {
		return out, err
	}
	return out, nil
}

// Instance is what EnsureInstance gives.
type Instance struct {
	Record  string `json:"instance"`
	Profile string `json:"profile"`
	Changed bool   `json:"changed"`
}

// EnsureInstance makes sure a step's instance and its profile are in the
// Control Plane's desired state, on host or the cluster's choice.
func EnsureInstance(args features.ServiceArgs, d Deps, s infra.Step, host string) (Instance, error) {
	profName := str(s.Desired, "profile")
	kind := str(s.Desired, "kind")
	if kind == "" {
		kind = "container"
	}
	profile := "profile_" + RecordID(profName)
	pv := map[string]any{"name": profName, "kind": kind}
	if img := str(s.Desired, "image"); img != "" {
		pv["image"] = img
	}
	if cpu, ok := s.Desired["cpu"].(float64); ok && cpu > 0 {
		pv["cpu"] = cpu
	}
	if mem, ok := s.Desired["memoryMb"].(float64); ok && mem > 0 {
		pv["memory"] = fmt.Sprintf("%dMB", int(mem))
	}
	if disk, ok := s.Desired["diskGb"].(float64); ok && disk > 0 {
		pv["disk"] = fmt.Sprintf("%dGB", int(disk))
	}
	for _, k := range []string{"packages", "units"} {
		if v, ok := s.Desired[k]; ok {
			pv[k] = v
		}
	}
	c1, err := d.Ensure(args, controlplane.EntityProfile, profile, pv, "infra-"+profile)
	if err != nil {
		return Instance{}, err
	}
	id := RecordID(s.ID)
	name := str(s.Desired, "instanceName")
	if name == "" {
		name = strings.TrimPrefix(id, "instance_")
	}
	iv := map[string]any{"name": name, "profile": profile, "kind": kind}
	if host != "" {
		iv["host"] = host
	}
	c2, err := d.Ensure(args, controlplane.EntityInstance, id, iv, "infra-"+id)
	if err != nil {
		return Instance{}, err
	}
	return Instance{Record: id, Profile: profile, Changed: c1 || c2}, nil
}

// NatsNode is what EnsureNatsNode gives.
type NatsNode struct {
	Instance string `json:"instance"`
	Unit     string `json:"unit"`
	Cluster  string `json:"cluster"`
	Changed  bool   `json:"changed"`
}

// EnsureNatsNode makes sure a natsNode step's node exists (forge 22h): its
// instance on its host (profile nats), its unit of kind nats and its
// site's infraNatsCluster record holding the nodes, gateways and leaf
// remotes the configuration gives, written whole by every node of the
// site, so the nodes may run in any order and at once.
func EnsureNatsNode(args features.ServiceArgs, d Deps, s infra.Step) (NatsNode, error) {
	host := RecordID(str(s.Desired, "host"))
	is := infra.Step{ID: s.ID, Kind: infra.KindInstance, Site: s.Site, Desired: map[string]any{"profile": "nats", "kind": "container", "instanceName": str(s.Desired, "name")}}
	inst, err := EnsureInstance(args, d, is, host)
	if err != nil {
		return NatsNode{}, err
	}
	unit, c1, err := EnsureUnit(args, d, inst.Record, infra.Unit{Name: "nats", Kind: "nats"})
	if err != nil {
		return NatsNode{}, err
	}
	rec := str(s.Desired, "record")
	if rec == "" {
		rec = infra.NatsClusterRecord(s.Site)
	}
	values := map[string]any{"name": s.Site, "site": s.Site}
	for _, k := range []string{"peers", "gateways", "leafRemotes"} {
		v := s.Desired[k]
		if v == nil {
			v = []any{}
		}
		f := k
		if k == "peers" {
			f = "nodes"
		}
		values[f] = v
	}
	c2, err := d.Ensure(args, controlplane.EntityNatsCluster, rec, values, "infra-"+rec)
	if err != nil {
		return NatsNode{}, err
	}
	return NatsNode{Instance: inst.Record, Unit: unit, Cluster: rec, Changed: inst.Changed || c1 || c2}, nil
}

// EnsureUnit makes sure an instance runs a unit (its infraUnit record).
func EnsureUnit(args features.ServiceArgs, d Deps, instance string, u infra.Unit) (string, bool, error) {
	if u.Name == "" || instance == "" {
		return "", false, fmt.Errorf("Ensure unit needs an instance and a unit with a name")
	}
	kind := u.Kind
	switch kind {
	case "serve", "nats", "adapter":
	default:
		kind = "other"
	}
	id := instance + "__" + RecordID(u.Name)
	v := map[string]any{"instance": instance, "name": u.Name, "kind": kind}
	if len(u.Services) > 0 {
		v["serviceSet"] = strings.Join(u.Services, ",")
	}
	if u.Exec != "" {
		v["config"] = map[string]any{"exec": u.Exec}
	}
	changed, err := d.Ensure(args, controlplane.EntityUnit, id, v, "infra-"+id)
	return id, changed, err
}

// EnsurePackages makes sure a profile or instance holds packages (name or
// name=version), one infraPackagePin each.
func EnsurePackages(args features.ServiceArgs, d Deps, target, source string, pkgs []string) ([]string, bool, error) {
	if source == "" {
		source = "ubuntu"
	}
	pins, changed := []string{}, false
	for _, p := range pkgs {
		name, version, _ := strings.Cut(p, "=")
		id := target + "__" + RecordID(name)
		v := map[string]any{"target": target, "name": name, "source": source}
		if version != "" {
			v["version"] = version
		}
		ch, err := d.Ensure(args, controlplane.EntityPackagePin, id, v, "infra-"+id)
		if err != nil {
			return nil, false, err
		}
		changed = changed || ch
		pins = append(pins, id)
	}
	return pins, changed, nil
}

// WaitHealthy waits until a host or instance record's health is up, or
// the timeout passes; it gives the last health.
func WaitHealthy(args features.ServiceArgs, d Deps, kind, id string, timeout time.Duration) (string, bool, error) {
	entity := controlplane.EntityHost
	if kind == infra.KindInstance {
		entity = controlplane.EntityInstance
	}
	deadline := time.Now().Add(timeout)
	health := "unknown"
	for {
		r, _, err := data.Get(args, d.Data, entity, id)
		if err != nil {
			return health, false, err
		}
		if r != nil {
			_ = json.Unmarshal(r["health"], &health)
		}
		if health == "up" {
			return health, true, nil
		}
		if time.Now().After(deadline) {
			return health, false, nil
		}
		select {
		case <-args.Context().Done():
			return health, false, args.Context().Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// RemoveStep makes sure what a step's resource id names is gone: a host
// at its provider and in the Control Plane, an instance's record.
func RemoveStep(args features.ServiceArgs, d Deps, s infra.Step, account infra.Account) (bool, error) {
	id := RecordID(s.ID)
	removed := false
	switch s.Kind {
	case infra.KindHost:
		if d.Compute != nil && account.Connection != "" {
			raw, err := d.Compute.Call(args, account, "host.delete", map[string]any{"id": str(s.Desired, "name")})
			if err != nil {
				return false, err
			}
			var r struct{ Deleted bool }
			_ = json.Unmarshal(raw, &r)
			removed = r.Deleted
		}
		ok, err := d.Remove(args, controlplane.EntityHost, id, "infra-"+id)
		return removed || ok, err
	case infra.KindInstance:
		return d.Remove(args, controlplane.EntityInstance, id, "infra-"+id)
	case infra.KindNatsNode:
		u, err := d.Remove(args, controlplane.EntityUnit, id+"__nats", "infra-"+id+"__nats")
		if err != nil {
			return false, err
		}
		ok, err := d.Remove(args, controlplane.EntityInstance, id, "infra-"+id)
		return u || ok, err
	}
	return false, nil // a cluster is its hosts and instances: nothing of its own yet (22h)
}
