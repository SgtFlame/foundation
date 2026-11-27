package natsauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// TestServer is an in-process NATS server running a fresh deployment's
// trust chain: the operator, SYS, CTL and APP preloaded into a full
// resolver, JetStream on, a TCP listener (forge 20a-2).
type TestServer struct {
	t   testing.TB
	ns  *server.Server
	URL string
	D   *Deployment
}

// StartTestSecure starts the server, stopped when the test ends.
func StartTestSecure(t testing.TB) *TestServer {
	t.Helper()
	d, err := Init("test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	conf := "listen: 127.0.0.1:-1\nserver_name: secure\njetstream: { store_dir: " + quote(filepath.Join(dir, "js")) + " }\nno_sys_acc: false\n" +
		d.ServerConfig(filepath.Join(dir, "resolver"))
	path := filepath.Join(dir, "nats.conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
	ns, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("the secure test server did not start")
	}
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	return &TestServer{t: t, ns: ns, URL: ns.ClientURL(), D: d}
}

func quote(s string) string { return "\"" + s + "\"" }

// CredsFile mints a user of the account and writes its creds file.
func (s *TestServer) CredsFile(account, name string, p Permissions, ttl time.Duration) (string, *User) {
	s.t.Helper()
	u, err := s.D.Account(account).NewUser(name, p, ttl)
	if err != nil {
		s.t.Fatal(err)
	}
	path := filepath.Join(s.t.TempDir(), name+".creds")
	if err := os.WriteFile(path, u.Creds, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path, u
}

// Conn connects as a new user of the account with the permissions.
func (s *TestServer) Conn(account, name string, p Permissions) (*nats.Conn, error) {
	path, _ := s.CredsFile(account, name, p, 0)
	return nats.Connect(s.URL, nats.UserCredentials(path), nats.Name(name), nats.MaxReconnects(0))
}

// Serve is natsd.Start as serve would connect, with a user of the
// application account allowed every subject in it.
func (s *TestServer) Serve() *natsd.Server {
	s.t.Helper()
	path, _ := s.CredsFile(App, "serve", Everything, 0)
	srv, err := natsd.Start(context.Background(), natsd.Options{URL: s.URL, Creds: path})
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(srv.Close)
	return srv
}

// System connects as a system account user, for pushing accounts.
func (s *TestServer) System() *nats.Conn {
	s.t.Helper()
	nc, err := s.Conn(System, "pusher", Everything)
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(nc.Close)
	return nc
}
