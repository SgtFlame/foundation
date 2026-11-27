package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/nats-io/nats.go/jetstream"
)

// NATS nodes (forge 20e): a unit of kind nats runs as axiom-nats@<unit>
// from one template unit, with /etc/nats/<unit>.conf rendered here from
// its cluster's record. A change is applied by a reload when NATS can
// reload everything that changed, else by a restart, and either way one
// node of a cluster at a time: the agent takes the cluster's lease in KV
// ctl_nats_rollout (create, expected revision, TTL), acts, waits until the
// node reports itself current (/healthz, which checks JetStream), then
// releases it. A change of the cluster's name or JetStream domain, which
// no node can take alone, is refused unless the cluster allows a full
// restart.

// NatsNode is a NATS node's configuration (controlplane.natsOf).
type NatsNode struct {
	Cluster          string          `json:"cluster"`
	Domain           string          `json:"domain,omitempty"`
	ServerName       string          `json:"serverName"`
	Host             string          `json:"host"`
	Routes           []string        `json:"routes"`
	Ports            map[string]int  `json:"ports"`
	Gateways         []NatsGateway   `json:"gateways,omitempty"`
	LeafRemotes      []NatsLeaf      `json:"leafRemotes,omitempty"`
	AllowFullRestart bool            `json:"allowFullRestart,omitempty"`
	Trust            *natsauth.Trust `json:"trust,omitempty"`
}

// NatsGateway is another cluster this one joins.
type NatsGateway struct {
	Name string   `json:"name"`
	URLs []string `json:"urls"`
}

// NatsLeaf is a hub a leaf site connects to.
type NatsLeaf struct {
	URL         string `json:"url"`
	Account     string `json:"account,omitempty"`
	Credentials string `json:"credentials,omitempty"`
}

// NatsTemplate is the NATS nodes' template unit.
const NatsTemplate = `[Unit]
Description=Axiom NATS %i
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/bin/nats-server -c /etc/nats/%i.conf
ExecReload=/bin/kill -HUP $MAINPID
StateDirectory=axiom/nats/%i
LimitNOFILE=65536
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// NatsSystemdName is a nats unit's systemd unit.
func NatsSystemdName(unit string) string { return "axiom-nats@" + unit }

// How long an agent waits for its cluster's rollout lease, and how long
// for its node to be ready (the lease lives ctl.RolloutTTL unrefreshed).
var (
	RolloutWait    = 15 * time.Minute
	NatsReadyAfter = 5 * time.Minute
)

// natsChange is a nats unit's configuration and what applying it takes:
// "" (nothing running to change, or nothing changed), reload or restart.
type natsChange struct {
	name    string
	unit    Unit
	systemd string
	node    NatsNode
	action  string
}

// natsUnits renders each nats unit with a cluster into axiom-nats@<unit>
// and its configuration file, deciding against what was last applied how
// a change is applied; a refused change keeps the applied configuration.
func (a *Agent) natsUnits(units []Unit) ([]Unit, []natsChange, []error) {
	out := make([]Unit, 0, len(units)+1)
	var changes []natsChange
	var errs []error
	for _, u := range units {
		if u.Kind != "nats" || u.Nats == nil {
			out = append(out, u)
			continue
		}
		node := *u.Nats
		action := ""
		if old, ok := a.applied(u.Name); ok {
			act, err := classify(old, node)
			if err != nil {
				errs = append(errs, err)
				node, act = old, ""
			}
			action = act
		}
		s := u
		s.Name = NatsSystemdName(u.Name)
		s.Files = append([]File{{Path: "/etc/nats/" + u.Name + ".conf", Content: RenderNats(u.Name, node), Mode: "0640", Raw: true}}, u.Files...)
		out = append(out, s)
		changes = append(changes, natsChange{name: u.Name, unit: s, systemd: s.Name, node: node, action: action})
	}
	if len(changes) > 0 {
		out = append(out, Unit{Kind: "template", Files: []File{{Path: "/etc/systemd/system/axiom-nats@.service", Content: NatsTemplate, Raw: true}}})
	}
	return out, changes, errs
}

// classify is how a node takes a change from old to new: NATS reloads
// routes and the trust chain; ports, the server's name, gateways and leaf
// remotes need the node restarted; the cluster's name and JetStream domain
// need every node at once, refused unless the cluster allows it (then
// still one at a time).
func classify(old, new NatsNode) (string, error) {
	same := func(a, b any) bool {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return string(x) == string(y)
	}
	if same(old, new) {
		return "", nil
	}
	if old.Cluster != new.Cluster || old.Domain != new.Domain {
		if !new.AllowFullRestart {
			return "", log.Code("nats_full_restart").Errorf("changing cluster %s's name or JetStream domain needs every node restarted together: set allowFullRestart on the cluster to roll it through anyway", old.Cluster)
		}
		return "restart", nil
	}
	if old.ServerName != new.ServerName || old.Host != new.Host || !same(old.Ports, new.Ports) || !same(old.Gateways, new.Gateways) || !same(old.LeafRemotes, new.LeafRemotes) {
		return "restart", nil
	}
	return "reload", nil
}

func (a *Agent) appliedFile(unit string) string {
	return filepath.Join(a.State, "nats", unit+".json")
}

func (a *Agent) applied(unit string) (NatsNode, bool) {
	var n NatsNode
	raw, err := os.ReadFile(a.appliedFile(unit))
	if err != nil {
		return n, false
	}
	return n, json.Unmarshal(raw, &n) == nil
}

func (a *Agent) saveApplied(c natsChange) {
	raw, _ := json.MarshalIndent(c.node, "", "  ")
	_ = os.MkdirAll(filepath.Dir(a.appliedFile(c.name)), 0o700)
	_ = os.WriteFile(a.appliedFile(c.name), raw, 0o600)
}

var leaseToken = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// rollout applies one node's change under its cluster's lease.
func (a *Agent) rollout(ctx context.Context, c natsChange) error {
	if a.Control == nil {
		return log.Code("not_connected").Errorf("the agent is not connected")
	}
	ctl0 := a.Control()
	if ctl0 == nil {
		return log.Code("not_connected").Errorf("the agent is not connected")
	}
	kv, err := ctl0.JS.KeyValue(ctx, ctl.RolloutBucket)
	if err != nil {
		return log.Wrapf(err, "the rollout bucket")
	}
	key := leaseToken.ReplaceAllString(c.node.Cluster, "_")
	if key == "" {
		key = "_"
	}
	holder, _ := json.Marshal(map[string]any{"instance": a.Instance, "unit": c.systemd, "action": c.action, "at": time.Now().UTC()})
	deadline := time.Now().Add(RolloutWait)
	var rev uint64
	for wait := 100 * time.Millisecond; ; {
		rev, err = kv.Create(ctx, key, holder)
		if err == nil {
			break
		}
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return log.Wrapf(err, "take cluster %s's rollout lease", c.node.Cluster)
		}
		if time.Now().After(deadline) {
			return log.Code("nats_rollout_busy").Errorf("cluster %s's rollout lease stayed taken", c.node.Cluster)
		}
		select {
		case <-ctx.Done():
			return log.Wrap(ctx.Err())
		case <-time.After(wait):
		}
		if wait < 2*time.Second {
			wait *= 2
		}
	}
	// Keep the lease while the node comes back
	done := make(chan struct{})
	refreshed := make(chan uint64, 1)
	go func() {
		r := rev
		for {
			select {
			case <-done:
				refreshed <- r
				return
			case <-time.After(ctl.RolloutTTL / 3):
				if n, err := kv.Update(ctx, key, holder, r); err == nil {
					r = n
				}
			}
		}
	}()
	err = a.apply(ctx, c)
	close(done)
	rev = <-refreshed
	if derr := kv.Delete(context.WithoutCancel(ctx), key, jetstream.LastRevision(rev)); derr != nil {
		log.With("cluster", c.node.Cluster).Warn(ctx, "agent: the rollout lease was not released (it expires): %v", derr)
	}
	return err
}

func (a *Agent) apply(ctx context.Context, c natsChange) error {
	if _, err := a.run(ctx, "systemctl", c.action, c.systemd); err != nil {
		return err
	}
	ready := a.NatsReady
	if ready == nil {
		ready = a.healthz
	}
	rctx, cancel := context.WithTimeout(ctx, NatsReadyAfter)
	defer cancel()
	if err := ready(rctx, c.unit); err != nil {
		return log.Code("nats_not_ready").Errorf("NATS node %s did not come back current after a %s: %v", c.systemd, c.action, err)
	}
	log.With("unit", c.systemd).Info(ctx, "agent: applied a %s to NATS node %s", c.action, c.systemd)
	return nil
}

// healthz waits until the node's monitoring port answers /healthz with
// 200: it is up and its JetStream current with the cluster.
func (a *Agent) healthz(ctx context.Context, u Unit) error {
	port := 8222
	if u.Nats != nil && u.Nats.Ports["monitor"] != 0 {
		port = u.Nats.Ports["monitor"]
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/healthz"
	var last error
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("healthz answered %d", resp.StatusCode)
		}
		last = err
		select {
		case <-ctx.Done():
			return last
		case <-time.After(time.Second):
		}
	}
}

// RenderNats is a node's nats-server.conf.
func RenderNats(unit string, n NatsNode) string {
	q := strconv.Quote
	port := func(k string, def int) int {
		if p := n.Ports[k]; p != 0 {
			return p
		}
		return def
	}
	store := "/var/lib/axiom/nats/" + unit
	var b strings.Builder
	b.WriteString("# Written by axiomd from desired state (forge 20e). Do not edit.\n")
	fmt.Fprintf(&b, "server_name: %s\n", q(n.ServerName))
	fmt.Fprintf(&b, "listen: \"0.0.0.0:%d\"\n", port("client", 4222))
	fmt.Fprintf(&b, "http: \"127.0.0.1:%d\"\n", port("monitor", 8222))
	fmt.Fprintf(&b, "jetstream {\n  store_dir: %s\n", q(store+"/js"))
	if n.Domain != "" {
		fmt.Fprintf(&b, "  domain: %s\n", q(n.Domain))
	}
	b.WriteString("}\n")
	if n.Cluster != "" {
		fmt.Fprintf(&b, "cluster {\n  name: %s\n  listen: \"0.0.0.0:%d\"\n  routes: [\n", q(n.Cluster), port("cluster", 6222))
		routes := append([]string(nil), n.Routes...)
		sort.Strings(routes)
		for _, r := range routes {
			fmt.Fprintf(&b, "    %s\n", q(r))
		}
		b.WriteString("  ]\n}\n")
	}
	if len(n.Gateways) > 0 {
		fmt.Fprintf(&b, "gateway {\n  name: %s\n  listen: \"0.0.0.0:%d\"\n  gateways: [\n", q(n.Cluster), port("gateway", 7222))
		for _, g := range n.Gateways {
			urls := make([]string, len(g.URLs))
			for i, u := range g.URLs {
				urls[i] = q(u)
			}
			fmt.Fprintf(&b, "    { name: %s, urls: [%s] }\n", q(g.Name), strings.Join(urls, ", "))
		}
		b.WriteString("  ]\n}\n")
	}
	fmt.Fprintf(&b, "leafnodes {\n  listen: \"0.0.0.0:%d\"\n", port("leaf", 7422))
	if len(n.LeafRemotes) > 0 {
		b.WriteString("  remotes: [\n")
		for _, l := range n.LeafRemotes {
			fmt.Fprintf(&b, "    { url: %s", q(l.URL))
			if l.Account != "" {
				fmt.Fprintf(&b, ", account: %s", q(l.Account))
			}
			if l.Credentials != "" {
				fmt.Fprintf(&b, ", credentials: %s", q(l.Credentials))
			}
			b.WriteString(" }\n")
		}
		b.WriteString("  ]\n")
	}
	b.WriteString("}\n")
	if t := n.Trust; t != nil && t.Operator != "" {
		fmt.Fprintf(&b, "operator: %s\nsystem_account: %s\n", q(t.Operator), q(t.SystemAccount))
		fmt.Fprintf(&b, "resolver {\n  type: full\n  dir: %s\n  allow_delete: false\n  interval: \"2m\"\n}\n", q(store+"/resolver"))
		keys := make([]string, 0, len(t.Preload))
		for k := range t.Preload {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("resolver_preload {\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, q(t.Preload[k]))
		}
		b.WriteString("}\n")
	}
	return b.String()
}
