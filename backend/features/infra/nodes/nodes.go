// Package infranodes holds the infrastructure blueprint nodes (forge 22e):
// each ensures one thing by its resource id, so running it twice changes
// nothing and running it after a crash continues. Hosts are made through
// infra.compute; everything else is the Control Plane's desired state, as
// records of the infrastructure app whose ids derive from resource ids,
// which the daemons reconcile toward.
package infranodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

const source = "infra"

// Deps are the nodes' run-time dependencies.
type Deps struct {
	Bus     infra.Executor
	Data    data.Deps
	Compute infra.Compute
}

// RecordID is the desired-state record id of a resource id: its words
// joined by _, which no configuration id holds, so ids never collide.
func RecordID(resource string) string {
	r := strings.NewReplacer(":", "_", "/", "_", ".", "_")
	id := r.Replace(resource)
	if len(id) > 120 {
		sum := sha256.Sum256([]byte(resource))
		id = id[:100] + "_" + hex.EncodeToString(sum[:8])
	}
	return id
}

// kinds the nodes run in: functions, server actions and triggers (and the
// jobs 22f enqueues, which run function graphs). Never commands: their
// effects are outside the decision.
var kinds = []string{cognition.KindFunction, livenodes.KindAction, livenodes.KindTrigger}

// settle waits until the records projection has folded every committed
// event, so a read after a write sees it.
func (d Deps) settle(args features.ServiceArgs) {
	if s, ok := any(d.Data.Projections).(interface {
		Settle(ctx context.Context, tenant uint64, projector string) error
	}); ok {
		_ = s.Settle(args.Context(), args.Principal.TenantID, data.Projector)
	}
}

// Ensure makes a record hold values, creating it under id when missing;
// it changes only what differs, so a second run sends nothing. key makes
// the commands' ids, so a retried step is dropped by Nats-Msg-Id.
func (d Deps) Ensure(args features.ServiceArgs, entity, id string, values map[string]any, key string) (bool, error) {
	if d.Bus == nil || d.Data.Projections == nil {
		return false, fmt.Errorf("the infrastructure nodes are not available")
	}
	d.settle(args)
	cur, _, err := data.Get(args, d.Data, entity, id)
	if err != nil {
		return false, err
	}
	if cur == nil {
		raw, _ := json.Marshal(map[string]any{"entity": entity, "values": values})
		r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-create", Type: "record.create", Target: id, Args: raw, Actor: "infra"})
		if r.Common.Code < 300 {
			return true, nil
		}
		if cur, _, err = data.Get(args, d.Data, entity, id); err != nil || cur == nil {
			return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
		}
	}
	set := map[string]any{}
	for _, k := range sortedKeys(values) {
		want, _ := json.Marshal(values[k])
		if !sameJSON(cur[k], want) {
			set[k] = values[k]
		}
	}
	if len(set) == 0 {
		return false, nil
	}
	raw, _ := json.Marshal(map[string]any{"entity": entity, "set": set})
	sum := sha256.Sum256(raw)
	r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-update-" + hex.EncodeToString(sum[:6]), Type: "record.update", Target: id, Args: raw, Actor: "infra"})
	if r.Common.Code >= 300 {
		return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
	}
	return true, nil
}

// Remove deletes a record if it exists.
func (d Deps) Remove(args features.ServiceArgs, entity, id, key string) (bool, error) {
	d.settle(args)
	cur, _, err := data.Get(args, d.Data, entity, id)
	if err != nil || cur == nil {
		return false, err
	}
	raw, _ := json.Marshal(map[string]any{"entity": entity})
	r := d.Bus.Execute(bus.Request{ServiceArgs: args, ID: key + "-delete", Type: "record.delete", Target: id, Args: raw, Actor: "infra"})
	if r.Common.Code >= 300 {
		return false, fmt.Errorf("%s %s: %s", entity, id, r.Common.Message)
	}
	return true, nil
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil {
		x = nil
	}
	if json.Unmarshal(b, &y) != nil {
		y = nil
	}
	ra, _ := json.Marshal(x)
	rb, _ := json.Marshal(y)
	return string(ra) == string(rb)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stepOf reads a plan step (or a resource) from a pin.
func stepOf(in cognition.Values, pin string) (infra.Step, error) {
	raw, _ := json.Marshal(in[pin])
	var s struct {
		infra.Step
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.ID == "" {
		return infra.Step{}, fmt.Errorf("%s is not a plan step: %s", pin, raw)
	}
	if s.Desired == nil {
		s.Desired = s.Props
	}
	if s.Desired == nil {
		s.Desired = s.Before
	}
	if s.Desired == nil {
		s.Desired = map[string]any{}
	}
	return s.Step, nil
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

const stepPin = "A plan step from infra.plan ({id, kind, site, account, desired}), or a resource ({id, kind, site, account, props})"

func node(reg *extensions.Registry, order int, t cognition.NodeType) {
	t.Source, t.Order, t.Category = source, order, "Infrastructure"
	t.External, t.Effects = true, true
	t.Kinds = kinds
	t.Tags = append(t.Tags, "infrastructure")
	cognition.Nodes.Register(reg, t)
}

// Contribute registers the nodes.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	node(reg, 1000, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.ensureHost"}, Title: "Ensure host",
		Description: "Makes sure a plan step's host exists at its provider (infra.compute host.create, keyed by its provider name, so a second run makes nothing) and that the Control Plane knows it (its infraHost record, by the resource id). Continues by Then with the host's record id and state (creating until the provider has it running; Wait healthy waits), or Failed with the provider's error.",
		Example:     "Ensure host for each create step of kind host, with the account's connection",
		Tags:        []string{"host", "compute", "ensure"},
		Sample:      cognition.Values{"host": "host_west_general_1", "state": "running", "address": "10.0.0.4", "message": ""},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("step", "Step", "json", stepPin),
				cognition.DataPin("connection", "Connection", "string", "The provider account's connection id"),
				cognition.DataPin("provider", "Provider", "string", "The account's provider (lxd, azure, digitalocean, fly, infratest)"),
				{ID: "userData", Name: "User data", Type: "string", Optional: true, Description: "Cloud-init user data for its first boot (the enrollment script)"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"), cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("host", "Host", "string", "The infraHost record's id"),
				cognition.DataPin("state", "State", "string", "The provider's state: creating, running, stopped"),
				cognition.DataPin("address", "Address", "string", "Its first address"),
				cognition.DataPin("message", "Message", "string", "Why it failed")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			if d.Compute == nil {
				return nil, "", fmt.Errorf("there is no compute connector")
			}
			conn, _ := cognition.Get[string](in, "connection")
			provider, _ := cognition.Get[string](in, "provider")
			userData, _ := cognition.Get[string](in, "userData")
			name := str(s.Desired, "name")
			args := map[string]any{"id": name, "size": str(s.Desired, "size"), "region": str(s.Desired, "region")}
			if img := str(s.Desired, "image"); img != "" {
				args["image"] = img
			}
			if userData != "" {
				args["userData"] = userData
			}
			if s.Desired["requiresVms"] == true {
				args["nested"] = true
			}
			raw, err := d.Compute.Call(x.ServiceArgs, infra.Account{ID: s.Account, Provider: provider, Connection: conn}, "host.create", args)
			if err != nil {
				return cognition.Values{"host": "", "state": "", "address": "", "message": err.Error()}, "failed", nil
			}
			var h infra.ProviderHost
			_ = json.Unmarshal(raw, &h)
			addr := ""
			if len(h.Addresses) > 0 {
				addr = h.Addresses[0]
			}
			id := RecordID(s.ID)
			values := map[string]any{"name": name, "site": s.Site, "provider": provider}
			if addr != "" {
				values["address"] = addr
			}
			if _, err := d.Ensure(x.ServiceArgs, controlplane.EntityHost, id, values, "infra-"+id); err != nil {
				return cognition.Values{"host": id, "state": h.State, "address": addr, "message": err.Error()}, "failed", nil
			}
			return cognition.Values{"host": id, "state": h.State, "address": addr, "message": ""}, cognition.Then, nil
		},
	})

	node(reg, 1010, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.ensureInstance"}, Title: "Ensure instance",
		Description: "Makes sure a plan step's instance is in the Control Plane's desired state: its profile (infraProfile, by the profile's id) and the instance (infraInstance, by the resource id) on a host, or left to the cluster when no host is given. The host's daemon makes it. Run twice, it changes nothing.",
		Example:     "Ensure instance for each create step of kind instance, on the host Ensure host gave",
		Tags:        []string{"instance", "container", "vm", "ensure"},
		Sample:      cognition.Values{"instance": "instance_west_web_web_1", "profile": "profile_web", "changed": true},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("step", "Step", "json", stepPin),
				{ID: "host", Name: "Host", Type: "string", Optional: true, Description: "The infraHost record to put it on; the cluster's choice when empty"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"),
				cognition.DataPin("instance", "Instance", "string", "The infraInstance record's id"),
				cognition.DataPin("profile", "Profile", "string", "The infraProfile record's id"),
				cognition.DataPin("changed", "Changed", "bool", "Whether anything was written")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			host, _ := cognition.Get[string](in, "host")
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
			c1, err := d.Ensure(x.ServiceArgs, controlplane.EntityProfile, profile, pv, "infra-"+profile)
			if err != nil {
				return nil, "", err
			}
			id := RecordID(s.ID)
			iv := map[string]any{"name": strings.TrimPrefix(id, "instance_"), "profile": profile, "kind": kind}
			if host != "" {
				iv["host"] = host
			}
			c2, err := d.Ensure(x.ServiceArgs, controlplane.EntityInstance, id, iv, "infra-"+id)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"instance": id, "profile": profile, "changed": c1 || c2}, cognition.Then, nil
		},
	})

	node(reg, 1020, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.ensureUnit"}, Title: "Ensure unit",
		Description: "Makes sure an instance runs a systemd unit: its infraUnit record (by the instance and the unit's name), with kind nats, serve, adapter or other, a serve unit's service set, and the command it runs. The instance's agent renders, enables and starts it.",
		Example:     "Ensure unit web (other, exec /usr/bin/web) on the instance Ensure instance gave",
		Tags:        []string{"unit", "systemd", "service", "ensure"},
		Sample:      cognition.Values{"unit": "instance_west_web_web_1__web", "changed": true},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("instance", "Instance", "string", "The infraInstance record's id"),
				cognition.DataPin("unit", "Unit", "json", "The unit: {name, kind (service, serve, nats, adapter), services (a serve unit's), exec}")},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"),
				cognition.DataPin("unit", "Unit", "string", "The infraUnit record's id"),
				cognition.DataPin("changed", "Changed", "bool", "Whether anything was written")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			inst, _ := cognition.Get[string](in, "instance")
			raw, _ := json.Marshal(in["unit"])
			var u infra.Unit
			if err := json.Unmarshal(raw, &u); err != nil || u.Name == "" || inst == "" {
				return nil, "", fmt.Errorf("Ensure unit needs an instance and a unit with a name")
			}
			kind := u.Kind
			switch kind {
			case "serve", "nats", "adapter":
			default:
				kind = "other"
			}
			id := inst + "__" + RecordID(u.Name)
			v := map[string]any{"instance": inst, "name": u.Name, "kind": kind}
			if len(u.Services) > 0 {
				v["serviceSet"] = strings.Join(u.Services, ",")
			}
			if u.Exec != "" {
				v["config"] = map[string]any{"exec": u.Exec}
			}
			changed, err := d.Ensure(x.ServiceArgs, controlplane.EntityUnit, id, v, "infra-"+id)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"unit": id, "changed": changed}, cognition.Then, nil
		},
	})

	node(reg, 1030, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.ensurePackages"}, Title: "Ensure packages",
		Description: "Makes sure a profile or instance holds packages: one infraPackagePin record each (by the target and the package's name), from Ubuntu or the first-party repository, at a version or following a channel. Packages left out are not removed.",
		Example:     "Ensure packages [nginx, jq] from ubuntu on the profile Ensure instance gave",
		Tags:        []string{"packages", "apt", "ensure"},
		Sample:      cognition.Values{"pins": []any{"profile_web__nginx"}, "changed": true},
		Config:      json.RawMessage(`{"type":"object","properties":{"source":{"type":"string","enum":["ubuntu","firstParty","thirdParty"],"title":"Source","description":"Where the packages come from; ubuntu when left out"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("target", "Target", "string", "The infraProfile or infraInstance record's id"),
				cognition.DataPin("packages", "Packages", "json", "Package names, each optionally name=version")},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"),
				cognition.DataPin("pins", "Pins", "json", "The infraPackagePin records' ids"),
				cognition.DataPin("changed", "Changed", "bool", "Whether anything was written")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			var c struct {
				Source string `json:"source"`
			}
			_ = x.Config(&c)
			if c.Source == "" {
				c.Source = "ubuntu"
			}
			target, _ := cognition.Get[string](in, "target")
			raw, _ := json.Marshal(in["packages"])
			var pkgs []string
			_ = json.Unmarshal(raw, &pkgs)
			pins, changed := []any{}, false
			for _, p := range pkgs {
				name, version, _ := strings.Cut(p, "=")
				id := target + "__" + RecordID(name)
				v := map[string]any{"target": target, "name": name, "source": c.Source}
				if version != "" {
					v["version"] = version
				}
				ch, err := d.Ensure(x.ServiceArgs, controlplane.EntityPackagePin, id, v, "infra-"+id)
				if err != nil {
					return nil, "", err
				}
				changed = changed || ch
				pins = append(pins, id)
			}
			return cognition.Values{"pins": pins, "changed": changed}, cognition.Then, nil
		},
	})

	node(reg, 1040, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.waitHealthy"}, Title: "Wait healthy",
		Description: "Waits until a host or instance record reports health up (its daemon's heartbeat), within a timeout (at most 5 minutes, ending with the run). Continues by Healthy, or Timed out with its last health.",
		Example:     "Wait healthy on the host Ensure host gave, for 120 seconds",
		Tags:        []string{"health", "wait"},
		Sample:      cognition.Values{"health": "up"},
		Config:      json.RawMessage(`{"type":"object","properties":{"timeoutSeconds":{"type":"integer","minimum":1,"maximum":300,"title":"Timeout","description":"Seconds to wait; 60 when left out"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("kind", "Kind", "string", "host or instance"),
				cognition.DataPin("id", "ID", "string", "The record's id")},
			Out: []cognition.Pin{cognition.ExecPin("healthy", "Healthy"), cognition.ExecPin("timedOut", "Timed out"),
				cognition.DataPin("health", "Health", "string", "Its last health: unknown, up or down")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			var c struct {
				TimeoutSeconds int `json:"timeoutSeconds"`
			}
			_ = x.Config(&c)
			if c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 300 {
				c.TimeoutSeconds = 60
			}
			kind, _ := cognition.Get[string](in, "kind")
			id, _ := cognition.Get[string](in, "id")
			entity := controlplane.EntityHost
			if kind == infra.KindInstance {
				entity = controlplane.EntityInstance
			}
			deadline := time.Now().Add(time.Duration(c.TimeoutSeconds) * time.Second)
			health := "unknown"
			for {
				r, _, err := data.Get(x.ServiceArgs, d.Data, entity, id)
				if err != nil {
					return nil, "", err
				}
				if r != nil {
					_ = json.Unmarshal(r["health"], &health)
				}
				if health == "up" {
					return cognition.Values{"health": health}, "healthy", nil
				}
				if time.Now().After(deadline) {
					return cognition.Values{"health": health}, "timedOut", nil
				}
				select {
				case <-x.ServiceArgs.Context().Done():
					return nil, "", x.ServiceArgs.Context().Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
		},
	})

	node(reg, 1050, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.remove"}, Title: "Remove",
		Description: "Makes sure the thing a plan step's resource id names is gone: a host is deleted at its provider (host.delete, idempotent) and its record removed; an instance's or cluster's record is removed, so its daemon removes it. Removing what is gone succeeds. Destructive: plans mark steps that hold data, and the apply asks first.",
		Example:     "Remove each remove step, hosts last",
		Tags:        []string{"remove", "delete", "destructive"},
		Sample:      cognition.Values{"removed": true, "message": ""},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("step", "Step", "json", stepPin),
				{ID: "connection", Name: "Connection", Type: "string", Optional: true, Description: "A host's provider account connection"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "Then"), cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("removed", "Removed", "bool", "Whether there was anything to remove"),
				cognition.DataPin("message", "Message", "string", "Why it failed")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			d := deps()
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			id := RecordID(s.ID)
			removed := false
			switch s.Kind {
			case infra.KindHost:
				if d.Compute != nil && s.Account != "" {
					conn, _ := cognition.Get[string](in, "connection")
					name := str(s.Desired, "name")
					raw, err := d.Compute.Call(x.ServiceArgs, infra.Account{ID: s.Account, Connection: conn}, "host.delete", map[string]any{"id": name})
					if err != nil {
						return cognition.Values{"removed": false, "message": err.Error()}, "failed", nil
					}
					var r struct{ Deleted bool }
					_ = json.Unmarshal(raw, &r)
					removed = r.Deleted
				}
				ok, err := d.Remove(x.ServiceArgs, controlplane.EntityHost, id, "infra-"+id)
				if err != nil {
					return cognition.Values{"removed": removed, "message": err.Error()}, "failed", nil
				}
				removed = removed || ok
			case infra.KindInstance:
				ok, err := d.Remove(x.ServiceArgs, controlplane.EntityInstance, id, "infra-"+id)
				if err != nil {
					return cognition.Values{"removed": false, "message": err.Error()}, "failed", nil
				}
				removed = ok
			default:
				// a cluster is its hosts and instances: nothing of its own to remove yet (22h)
			}
			return cognition.Values{"removed": removed, "message": ""}, cognition.Then, nil
		},
	})
}
