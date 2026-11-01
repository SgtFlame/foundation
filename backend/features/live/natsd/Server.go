// Package natsd runs the embedded NATS server with JetStream that the live
// core keeps its event streams and KV buckets in (documentation/Live.md ›
// Embedded NATS). serve starts it before the services, in-process and with
// no network listener, and drains it on shutdown.
package natsd

import (
	"context"
	"path/filepath"
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
	// URL connects to an external NATS server instead of embedding one.
	URL string
	// Listen, when set (e.g. "127.0.0.1:4222"), lets other processes
	// connect to the embedded server; empty listens nowhere.
	Listen string
}

// Server is a connection to NATS JetStream, embedded or external.
type Server struct {
	ns *server.Server // nil for an external server
	nc *nats.Conn
	js jetstream.JetStream
}

// Start starts the embedded server (or connects to opts.URL) and waits
// until JetStream is ready.
func Start(ctx context.Context, opts Options) (*Server, error) {
	if opts.URL != "" {
		nc, err := nats.Connect(opts.URL, nats.Name("foundation"), nats.MaxReconnects(-1))
		if err != nil {
			return nil, log.Wrapf(err, "connect to NATS at %s", opts.URL)
		}
		return connected(nil, nc)
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
	s, err := connected(ns, nc)
	if err != nil {
		return nil, err
	}
	log.Info(ctx, "embedded NATS JetStream in %s", so.StoreDir)
	return s, nil
}

func connected(ns *server.Server, nc *nats.Conn) (*Server, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		if ns != nil {
			ns.Shutdown()
		}
		return nil, log.Wrap(err)
	}
	return &Server{ns: ns, nc: nc, js: js}, nil
}

// Conn is the core NATS connection, for plain subjects.
func (s *Server) Conn() *nats.Conn { return s.nc }

// JS is the JetStream context.
func (s *Server) JS() jetstream.JetStream { return s.js }

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

// StartTest starts an embedded server in a temporary directory, closed
// when the test ends.
func StartTest(t testing.TB) *Server {
	t.Helper()
	s, err := Start(context.Background(), Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
