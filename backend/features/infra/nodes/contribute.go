package infranodes

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

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
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			conn, _ := cognition.Get[string](in, "connection")
			provider, _ := cognition.Get[string](in, "provider")
			userData, _ := cognition.Get[string](in, "userData")
			h, err := EnsureHost(x.ServiceArgs, deps(), s, infra.Account{ID: s.Account, Provider: provider, Connection: conn}, userData)
			out := cognition.Values{"host": h.Record, "state": h.State, "address": h.Address, "message": ""}
			if err != nil {
				out["message"] = err.Error()
				return out, "failed", nil
			}
			return out, cognition.Then, nil
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
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			host, _ := cognition.Get[string](in, "host")
			i, err := EnsureInstance(x.ServiceArgs, deps(), s, host)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"instance": i.Record, "profile": i.Profile, "changed": i.Changed}, cognition.Then, nil
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
			inst, _ := cognition.Get[string](in, "instance")
			raw, _ := json.Marshal(in["unit"])
			var u infra.Unit
			_ = json.Unmarshal(raw, &u)
			id, changed, err := EnsureUnit(x.ServiceArgs, deps(), inst, u)
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
			var c struct {
				Source string `json:"source"`
			}
			_ = x.Config(&c)
			target, _ := cognition.Get[string](in, "target")
			raw, _ := json.Marshal(in["packages"])
			var pkgs []string
			_ = json.Unmarshal(raw, &pkgs)
			pins, changed, err := EnsurePackages(x.ServiceArgs, deps(), target, c.Source, pkgs)
			if err != nil {
				return nil, "", err
			}
			list := make([]any, len(pins))
			for i, p := range pins {
				list[i] = p
			}
			return cognition.Values{"pins": list, "changed": changed}, cognition.Then, nil
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
			var c struct {
				TimeoutSeconds int `json:"timeoutSeconds"`
			}
			_ = x.Config(&c)
			if c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 300 {
				c.TimeoutSeconds = 60
			}
			kind, _ := cognition.Get[string](in, "kind")
			id, _ := cognition.Get[string](in, "id")
			health, ok, err := WaitHealthy(x.ServiceArgs, deps(), kind, id, time.Duration(c.TimeoutSeconds)*time.Second)
			if err != nil {
				return nil, "", err
			}
			if ok {
				return cognition.Values{"health": health}, "healthy", nil
			}
			return cognition.Values{"health": health}, "timedOut", nil
		},
	})

	node(reg, 1050, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "infra.remove"}, Title: "Remove",
		Description: "Makes sure the thing a plan step's resource id names is gone: a host is deleted at its provider (host.delete, idempotent) and its record removed; an instance's record is removed, so its daemon removes it. Removing what is gone succeeds. Destructive: plans mark steps that hold data, and the apply asks first.",
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
			s, err := stepOf(in, "step")
			if err != nil {
				return nil, "", err
			}
			conn, _ := cognition.Get[string](in, "connection")
			removed, err := RemoveStep(x.ServiceArgs, deps(), s, infra.Account{ID: s.Account, Connection: conn})
			if err != nil {
				return cognition.Values{"removed": removed, "message": err.Error()}, "failed", nil
			}
			return cognition.Values{"removed": removed, "message": ""}, cognition.Then, nil
		},
	})
}
