// Package natsd runs the embedded NATS server with JetStream that the live
// core keeps its event streams and KV buckets in (documentation/Live.md ›
// Embedded NATS). serve starts it before the services, in-process and with
// no network listener, and drains it on shutdown.
package natsd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var log = logging.In("live").Tags("nats")

// Options configure Start.
type Options struct {
	// Dir holds JetStream's data, <data-dir>/nats in serve.
	Dir string
	// URL connects to an external NATS server instead of embedding one;
	// a deployed cluster is several URLs, comma separated (forge 20a-1).
	URL string
	// Creds is a NATS credentials file (a user JWT and its seed) for an
	// external server; CA is the deployment's CA certificate for TLS.
	Creds string
	// CredsData is a creds file's text, in place of Creds (a user minted
	// in memory, forge 20b-2).
	CredsData []byte
	CA        string
	// Listen, when set (e.g. "127.0.0.1:4222"), lets other processes
	// connect to the embedded server; empty listens nowhere.
	Listen string
}

// Server is a connection to NATS JetStream, embedded or external.
type Server struct {
	ns   *server.Server // nil for an external server
	nc   *nats.Conn
	js   jetstream.JetStream
	opts Options
}

// connectOptions are the options every connection to an external server
// uses: its credentials and CA, and reconnecting forever.
func (o Options) connectOptions(name string) []nats.Option {
	opts := []nats.Option{nats.Name(name), nats.MaxReconnects(-1)}
	if o.Creds != "" {
		opts = append(opts, nats.UserCredentials(o.Creds))
	}
	if len(o.CredsData) > 0 {
		opts = append(opts, nats.UserJWTAndSeed(splitCreds(o.CredsData)))
	}
	if o.CA != "" {
		opts = append(opts, nats.RootCAs(o.CA))
	}
	return opts
}

// Start starts the embedded server (or connects to opts.URL) and waits
// until JetStream is ready.
func Start(ctx context.Context, opts Options) (*Server, error) {
	if opts.URL != "" {
		nc, err := nats.Connect(opts.URL, opts.connectOptions("foundation")...)
		if err != nil {
			return nil, log.Wrapf(err, "connect to NATS at %s", opts.URL)
		}
		return connected(nil, nc, opts)
	}
	so := &server.Options{
		ServerName: "foundation",
		JetStream:  true,
		StoreDir:   filepath.Clean(opts.Dir),
		DontListen: opts.Listen == "",
		NoSigs:     true,
		NoLog:      true,
	}
	if opts.Listen != "" {
		host, port, err := splitHostPort(opts.Listen)
		if err != nil {
			return nil, err
		}
		so.Host, so.Port = host, port
	}
	ns, err := server.NewServer(so)
	if err != nil {
		return nil, log.Wrapf(err, "make the embedded NATS server")
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		return nil, log.Errorf("the embedded NATS server did not start")
	}
	nc, err := nats.Connect("", nats.InProcessServer(ns), nats.Name("foundation"))
	if err != nil {
		ns.Shutdown()
		return nil, log.Wrapf(err, "connect to the embedded NATS server")
	}
	s, err := connected(ns, nc, opts)
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "embedded NATS JetStream in %s", so.StoreDir)
	return s, nil
}

func connected(ns *server.Server, nc *nats.Conn, opts Options) (*Server, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		if ns != nil {
			ns.Shutdown()
		}
		return nil, log.Wrap(err)
	}
	return &Server{ns: ns, nc: nc, js: js, opts: opts}, nil
}

// Conn is the core NATS connection, for plain subjects.
func (s *Server) Conn() *nats.Conn { return s.nc }

// JS is the JetStream context.
func (s *Server) JS() jetstream.JetStream { return s.js }

// Connect opens another connection to the same server, as another
// process would (an external connector adapter, forge 07b): in-process to
// the embedded server, else to the external one's URL.
func (s *Server) Connect(name string) (*nats.Conn, error) {
	if s.ns != nil {
		return nats.Connect("", nats.InProcessServer(s.ns), nats.Name(name))
	}
	url := s.opts.URL
	if url == "" {
		url = s.nc.ConnectedUrl()
	}
	return nats.Connect(url, s.opts.connectOptions(name)...)
}

// ClientURL is where another process connects: the embedded server's
// listener (empty when it listens nowhere), or the external server's URL.
func (s *Server) ClientURL() string {
	if s.ns != nil {
		if s.ns.Addr() == nil {
			return ""
		}
		return s.ns.ClientURL()
	}
	return s.nc.ConnectedUrl()
}

// Close drains the connection and stops the embedded server.
func (s *Server) Close() {
	if err := s.nc.Drain(); err != nil {
		s.nc.Close()
	}
	for i := 0; i < 50 && !s.nc.IsClosed(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if s.ns != nil {
		s.ns.Shutdown()
		s.ns.WaitForShutdown()
	}
}

// StartTestListening is StartTest with a TCP listener on a free local
// port, for tests that connect from another process (ClientURL).
func StartTestListening(t testing.TB) *Server {
	t.Helper()
	s, err := Start(context.Background(), Options{Dir: t.TempDir(), Listen: "127.0.0.1:-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// StartTest starts an embedded server in a temporary directory, closed
// when the test ends. With $FOUNDATION_TEST_NATS_CLUSTER set to a node
// count (3), it connects to a fresh cluster of that many nodes instead and
// makes everything with that many replicas, so any package's tests run
// against deployed NATS (forge 20a-1).
func StartTest(t testing.TB) *Server {
	t.Helper()
	if n, err := strconv.Atoi(os.Getenv("FOUNDATION_TEST_NATS_CLUSTER")); err == nil && n > 0 {
		SetDefault(Placement{Replicas: n})
		return StartTestCluster(t, n).Connect()
	}
	s, err := Start(context.Background(), Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// ConnectOptions are the options a connection to the deployment as o
// describes takes (its name, creds, CA and reconnects), for tools that
// connect without JetStream.
func ConnectOptions(o Options, name string) []nats.Option { return o.connectOptions(name) }
