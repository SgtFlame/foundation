package natsd

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
)

// Supercluster is several test clusters joined by gateways (forge 20a-4),
// one per LXD cluster in a deployment; the first is the hub on-premise
// sites connect to as leaf nodes (part question 1: the root tenant's
// cluster).
type Supercluster struct {
	t        testing.TB
	Clusters []*Cluster
	leaf     []int // the hub's leaf node ports
}

// StartTestSupercluster starts n clusters of size nodes, named a, b, …,
// joined by gateways, stopped when the test ends.
func StartTestSupercluster(t testing.TB, n, size int) *Supercluster {
	t.Helper()
	sc := &Supercluster{t: t}
	names := make([]string, n)
	gw := make([][]int, n)
	for i := range names {
		names[i] = string(rune('a' + i))
		gw[i] = freePorts(t, size)
	}
	sc.leaf = freePorts(t, size)
	for ci, name := range names {
		c := newTestCluster(t, name, size, func(i int, o *server.Options) {
			withSystemAccount(o)
			if n > 1 {
				o.Gateway = server.GatewayOpts{Name: name, Host: "127.0.0.1", Port: gw[ci][i]}
				for oi, other := range names {
					if oi == ci {
						continue
					}
					var urls []*url.URL
					for _, p := range gw[oi] {
						urls = append(urls, &url.URL{Scheme: "nats", Host: fmt.Sprintf("127.0.0.1:%d", p)})
					}
					o.Gateway.Gateways = append(o.Gateway.Gateways, &server.RemoteGatewayOpts{Name: other, URLs: urls})
				}
			}
			if ci == 0 {
				o.LeafNode = server.LeafNodeOpts{Host: "127.0.0.1", Port: sc.leaf[i]}
			}
		})
		sc.Clusters = append(sc.Clusters, c)
	}
	// every cluster's nodes start before any waits: the gateways connect
	// as their peers come up
	for _, c := range sc.Clusters {
		for i := range c.opts {
			c.start(i)
		}
		t.Cleanup(c.Shutdown)
	}
	for _, c := range sc.Clusters {
		c.WaitReady()
	}
	return sc
}

// Hub is the cluster sites connect to.
func (sc *Supercluster) Hub() *Cluster { return sc.Clusters[0] }

// Site is an on-premise site joined to the hub as a leaf node: its own
// JetStream domain, so its streams stay at the site, and a link that can
// be cut and restored.
type Site struct {
	*Cluster
	Domain string
	Link   *Link
}

// StartTestSite starts a site of n nodes (1, a single leaf node, or a
// cluster of its own, iteration question 4) in the JetStream domain, each
// node holding a leaf connection to the hub through one Link.
func (sc *Supercluster) StartTestSite(name, domain string, n int) *Site {
	t := sc.t
	t.Helper()
	var hub []string
	for _, p := range sc.leaf {
		hub = append(hub, fmt.Sprintf("127.0.0.1:%d", p))
	}
	link := newLink(t, hub[0])
	c := newTestCluster(t, name, n, func(i int, o *server.Options) {
		withSystemAccount(o)
		o.JetStreamDomain = domain
		if n == 1 {
			o.Cluster, o.Routes = server.ClusterOpts{}, nil
		}
		o.LeafNode = server.LeafNodeOpts{Remotes: []*server.RemoteLeafOpts{{
			URLs: []*url.URL{{Scheme: "nats-leaf", Host: link.Addr()}},
		}}, ReconnectInterval: 200_000_000}
	})
	c.startAll()
	return &Site{Cluster: c, Domain: domain, Link: link}
}

// Link is a TCP relay standing for a site's network link to the hub:
// Cut drops every connection and refuses new ones until Restore.
type Link struct {
	t     testing.TB
	l     net.Listener
	to    string
	mu    sync.Mutex
	cut   bool
	conns []net.Conn
}

func newLink(t testing.TB, to string) *Link {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	k := &Link{t: t, l: l, to: to}
	t.Cleanup(func() { l.Close(); k.Cut() })
	go k.serve()
	return k
}

// Addr is the address sites dial for the hub.
func (k *Link) Addr() string { return k.l.Addr().String() }

func (k *Link) serve() {
	for {
		in, err := k.l.Accept()
		if err != nil {
			return
		}
		k.mu.Lock()
		if k.cut {
			k.mu.Unlock()
			in.Close()
			continue
		}
		out, err := net.Dial("tcp", k.to)
		if err != nil {
			k.mu.Unlock()
			in.Close()
			continue
		}
		k.conns = append(k.conns, in, out)
		k.mu.Unlock()
		go func() { _, _ = io.Copy(out, in); out.Close(); in.Close() }()
		go func() { _, _ = io.Copy(in, out); out.Close(); in.Close() }()
	}
}

// Cut drops the link.
func (k *Link) Cut() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cut = true
	for _, c := range k.conns {
		c.Close()
	}
	k.conns = nil
}

// Restore lets the site connect again.
func (k *Link) Restore() {
	k.mu.Lock()
	k.cut = false
	k.mu.Unlock()
}

// withSystemAccount gives a node the system account that gateways and
// leaf nodes need; clients without credentials stay in the global account.
func withSystemAccount(o *server.Options) {
	o.Accounts = []*server.Account{server.NewAccount("SYS")}
	o.SystemAccount = "SYS"
}
