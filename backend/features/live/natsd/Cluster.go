package natsd

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Cluster is a JetStream cluster of in-process servers on loopback ports,
// for tests of what must survive losing a node (forge 20a-1): each node
// has its own store directory, so a stopped node restarts with its data.
type Cluster struct {
	t     testing.TB
	name  string
	opts  []*server.Options
	nodes []*server.Server
}

// StartTestCluster starts n clustered JetStream servers, stopped when the
// test ends. Streams made with replicas up to n survive one node stopped.
func StartTestCluster(t testing.TB, n int) *Cluster {
	t.Helper()
	c := newTestCluster(t, fmt.Sprintf("test-%d", time.Now().UnixNano()), n, nil)
	c.startAll()
	return c
}

// newTestCluster prepares n nodes named <name>-n<i>, clustered when n > 1;
// configure adjusts each node's options before it starts.
func newTestCluster(t testing.TB, name string, n int, configure func(i int, o *server.Options)) *Cluster {
	t.Helper()
	c := &Cluster{t: t, name: name}
	clientPorts, routePorts := freePorts(t, n), freePorts(t, n)
	var routes []*url.URL
	for _, p := range routePorts {
		routes = append(routes, &url.URL{Scheme: "nats-route", Host: fmt.Sprintf("127.0.0.1:%d", p)})
	}
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		o := &server.Options{
			ServerName: fmt.Sprintf("%s-n%d", name, i),
			Host:       "127.0.0.1",
			Port:       clientPorts[i],
			JetStream:  true,
			StoreDir:   filepath.Join(dir, fmt.Sprintf("n%d", i)),
			// explicit limits: the defaults, a share of the machine's,
			// refuse a test's many replicated streams as too big
			JetStreamMaxMemory: 256 << 20,
			JetStreamMaxStore:  1 << 30,
			NoSigs:             true,
			NoLog:              true,
			Cluster: server.ClusterOpts{
				Name: name,
				Host: "127.0.0.1",
				Port: routePorts[i],
			},
			Routes: routes,
		}
		if configure != nil {
			configure(i, o)
		}
		c.opts = append(c.opts, o)
		c.nodes = append(c.nodes, nil)
	}
	return c
}

func (c *Cluster) startAll() {
	c.t.Helper()
	for i := range c.opts {
		c.start(i)
	}
	c.t.Cleanup(c.Shutdown)
	c.WaitReady()
}

// Name is the cluster's name, as a Placement's Cluster names it.
func (c *Cluster) Name() string { return c.name }

func (c *Cluster) start(i int) {
	c.t.Helper()
	ns, err := server.NewServer(c.opts[i])
	if err != nil {
		c.t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		c.t.Fatalf("node %d did not start", i)
	}
	c.nodes[i] = ns
}

// WaitReady waits until JetStream has a meta leader every running node
// knows, so streams can be made.
func (c *Cluster) WaitReady() {
	c.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ok := true
		for _, ns := range c.nodes {
			if ns != nil && !ns.JetStreamIsCurrent() {
				ok = false
			}
		}
		if ok {
			nc, err := nats.Connect(c.URL())
			if err == nil {
				_, err = nc.Request("$JS.API.INFO", nil, time.Second)
				nc.Close()
				if err == nil {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatal("the test cluster has no JetStream leader")
}

// URL is every running node's client URL, comma separated, as serve's
// --nats-url takes it.
func (c *Cluster) URL() string {
	var urls []string
	for i, ns := range c.nodes {
		if ns != nil {
			urls = append(urls, fmt.Sprintf("nats://127.0.0.1:%d", c.opts[i].Port))
		}
	}
	return strings.Join(urls, ",")
}

// AllURLs is every node's client URL, running or not, for clients that
// must reconnect to a node that comes back.
func (c *Cluster) AllURLs() string {
	var urls []string
	for _, o := range c.opts {
		urls = append(urls, fmt.Sprintf("nats://127.0.0.1:%d", o.Port))
	}
	return strings.Join(urls, ",")
}

// Connect is Start against the cluster, as serve with --nats-url.
func (c *Cluster) Connect() *Server {
	c.t.Helper()
	s, err := Start(context.Background(), Options{URL: c.AllURLs()})
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(s.Close)
	return s
}

// Size is the number of nodes.
func (c *Cluster) Size() int { return len(c.nodes) }

// Running tells whether node i is up.
func (c *Cluster) Running(i int) bool { return c.nodes[i] != nil }

// Stop stops node i, keeping its store for Restart.
func (c *Cluster) Stop(i int) {
	if ns := c.nodes[i]; ns != nil {
		ns.Shutdown()
		ns.WaitForShutdown()
		c.nodes[i] = nil
	}
}

// Restart starts a stopped node i again on its ports and store.
func (c *Cluster) Restart(i int) {
	c.t.Helper()
	if c.nodes[i] == nil {
		c.start(i)
	}
}

// Shutdown stops every node.
func (c *Cluster) Shutdown() {
	for i := range c.nodes {
		c.Stop(i)
	}
}

func freePorts(t testing.TB, n int) []int {
	t.Helper()
	var ports []int
	var ls []net.Listener
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ls = append(ls, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range ls {
		l.Close()
	}
	return ports
}

// NodeName is node i's server name, as a stream's leader names it.
func (c *Cluster) NodeName(i int) string { return c.opts[i].ServerName }
