package agent

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fake struct {
	t     *testing.T
	a     *Agent
	state string
}

// newFake is an agent whose apt-get, apt-mark, dpkg-query and systemctl
// are the fakes in testdata/bin, keeping their state in a directory, and
// whose files go under a temporary root: no root needed.
func newFake(t *testing.T) *fake {
	t.Helper()
	bin := t.TempDir()
	for _, n := range []string{"apt-get", "apt-mark", "dpkg-query", "systemctl"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "bin", n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, n), raw, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	return &fake{t: t, state: state, a: &Agent{Root: t.TempDir(), State: t.TempDir(), Path: bin + ":/usr/bin:/bin", Env: []string{"FAKE_STATE=" + state}}}
}

func (f *fake) repo(name string, versions ...string) {
	_ = os.MkdirAll(filepath.Join(f.state, "repo"), 0o755)
	_ = os.WriteFile(filepath.Join(f.state, "repo", name), []byte(strings.Join(versions, "\n")+"\n"), 0o644)
}

func (f *fake) reconcile(doc any) error {
	raw, _ := json.Marshal(doc)
	return f.a.Reconcile(context.Background(), raw)
}

func (f *fake) calls() []string {
	raw, _ := os.ReadFile(filepath.Join(f.state, "calls"))
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (f *fake) count(prefix string) int {
	n := 0
	for _, c := range f.calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fake) version(name string) string {
	raw, _ := os.ReadFile(filepath.Join(f.state, "pkg", name))
	return strings.TrimSpace(string(raw))
}

func (f *fake) held(name string) bool {
	_, err := os.Stat(filepath.Join(f.state, "held", name))
	return err == nil
}

// TestPackages: a pin installs once and is held; a version change
// upgrades and holds again; a removed pin uninstalls only Axiom's
// package, never one it did not install.
func TestPackages(t *testing.T) {
	f := newFake(t)
	f.repo("nginx", "1.24.0-1", "1.24.0-2")
	f.repo("curl", "8.5.0")
	_ = os.MkdirAll(filepath.Join(f.state, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(f.state, "pkg", "openssh-server"), []byte("9.6"), 0o644) // not Axiom's
	doc := map[string]any{"pins": []Pin{{Name: "nginx", Source: "ubuntu", Version: "1.24.0-1"}, {Name: "curl", Source: "ubuntu", Version: "8.5.0"}}}
	for i := 0; i < 3; i++ {
		if err := f.reconcile(doc); err != nil {
			t.Fatal(err)
		}
	}
	if f.count("apt-get install") != 2 || f.version("nginx") != "1.24.0-1" || !f.held("nginx") {
		t.Fatalf("installs %v", f.calls())
	}
	doc["pins"] = []Pin{{Name: "nginx", Source: "ubuntu", Version: "1.24.0-2"}}
	if err := f.reconcile(doc); err != nil {
		t.Fatal(err)
	}
	if f.version("nginx") != "1.24.0-2" || !f.held("nginx") {
		t.Fatalf("upgrade: %s %v", f.version("nginx"), f.calls())
	}
	if f.version("curl") != "" || f.version("openssh-server") != "9.6" {
		t.Fatalf("removal: curl %q openssh %q", f.version("curl"), f.version("openssh-server"))
	}
	facts := f.a.Facts(context.Background())
	if pk, _ := facts["packages"].(map[string]string); pk["nginx"] != "1.24.0-2" || len(pk) != 1 {
		t.Fatalf("facts %v", facts["packages"])
	}
}

// TestFilesAndUnits: a unit's file is rendered from its config, its unit
// is enabled and started once, restarted when its file changes, and
// stopped when desired stopped.
func TestFilesAndUnits(t *testing.T) {
	f := newFake(t)
	f.repo("nginx", "1.24.0-1")
	unit := func(port int, status string) map[string]any {
		return map[string]any{"units": []map[string]any{{
			"name": "nginx", "package": "nginx", "version": "1.24.0-1", "status": status,
			"config": map[string]any{"port": port},
			"files": []File{
				{Path: "/etc/nginx/conf.d/axiom.conf", Content: "server { listen {{.Config.port}}; }\n", Mode: "0640"},
				{Path: "/etc/systemd/system/nginx.service.d/axiom.conf", Content: "[Service]\nRestart=always\n"},
			},
		}}}
	}
	for i := 0; i < 2; i++ {
		if err := f.reconcile(unit(80, "running")); err != nil {
			t.Fatal(err)
		}
	}
	conf := filepath.Join(f.a.Root, "etc/nginx/conf.d/axiom.conf")
	raw, _ := os.ReadFile(conf)
	if string(raw) != "server { listen 80; }\n" {
		t.Fatalf("rendered %q", raw)
	}
	if st, _ := os.Stat(conf); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode())
	}
	if f.count("systemctl enable --now nginx") != 1 || f.count("systemctl daemon-reload") != 1 || f.count("systemctl restart") != 0 {
		t.Fatalf("first runs %v", f.calls())
	}
	if err := f.reconcile(unit(8080, "running")); err != nil {
		t.Fatal(err)
	}
	if f.count("systemctl restart nginx") != 1 || f.count("systemctl daemon-reload") != 1 {
		t.Fatalf("a changed file restarts its unit: %v", f.calls())
	}
	if err := f.reconcile(unit(8080, "stopped")); err != nil {
		t.Fatal(err)
	}
	if f.count("systemctl disable --now nginx") != 1 {
		t.Fatalf("stop: %v", f.calls())
	}
	if us, _ := f.a.Facts(context.Background())["units"].(map[string]string); us["nginx"] != "inactive" {
		t.Fatalf("unit facts %v", us)
	}
}

// TestHealth: unit, tcp and nats checks; a failing one is reported.
func TestHealth(t *testing.T) {
	f := newFake(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("INFO {}\r\n"))
			c.Close()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	doc := map[string]any{"units": []map[string]any{
		{"name": "web", "health": []Check{{Kind: "tcp", Port: port}}},
		{"name": "bus", "health": []Check{{Kind: "nats", Port: port}}},
		{"name": "db", "health": []Check{{Kind: "tcp", Port: dead}}},
		{"name": "plain"},
	}}
	if err := f.reconcile(doc); err != nil {
		t.Fatal(err)
	}
	h := f.a.Health(context.Background())
	if h["web"] != "up" || h["bus"] != "up" || h["plain"] != "up" || !strings.HasPrefix(h["db"], "down") {
		t.Fatalf("health %v", h)
	}
}

// TestSourcesAllowList: an approved source is written with its key; one
// not approved is refused, and so are the pins that name it.
func TestSourcesAllowList(t *testing.T) {
	f := newFake(t)
	f.repo("vendor-tool", "2.0")
	doc := map[string]any{
		"sources": []Source{
			{ID: "s1", Name: "vendor", URIs: "https://apt.vendor.example", Suites: "noble", Components: "main", Key: "-----BEGIN PGP PUBLIC KEY BLOCK-----\n", Approved: true},
			{ID: "s2", Name: "sketchy", URIs: "https://ppa.example", Suites: "noble", Components: "main"},
		},
		"pins": []Pin{{Name: "vendor-tool", Source: "thirdParty", Repository: "s1", Version: "2.0"}, {Name: "sketchy-tool", Source: "thirdParty", Repository: "s2"}},
	}
	err := f.reconcile(doc)
	if err == nil || !strings.Contains(err.Error(), "sketchy") {
		t.Fatalf("the unapproved source was not refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.a.Root, "etc/apt/sources.list.d/axiom-sketchy.sources")); err == nil {
		t.Fatal("wrote an unapproved source")
	}
	raw, _ := os.ReadFile(filepath.Join(f.a.Root, "etc/apt/sources.list.d/axiom-vendor.sources"))
	if !strings.Contains(string(raw), "Signed-By: /etc/apt/keyrings/axiom-vendor.asc") {
		t.Fatalf("source file %q", raw)
	}
	if f.version("vendor-tool") != "2.0" || f.count("apt-get install -y --no-install-recommends --allow-change-held-packages --allow-downgrades sketchy") != 0 {
		t.Fatalf("installs %v", f.calls())
	}
	if f.count("apt-get update") != 1 {
		t.Fatalf("updates %v", f.calls())
	}
	// Dropped from desired state: its files go
	if err := f.reconcile(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.a.Root, "etc/apt/sources.list.d/axiom-vendor.sources")); err == nil {
		t.Fatal("kept a source no longer wanted")
	}
}
