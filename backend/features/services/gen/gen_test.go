package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// root is the module root: backend, three levels up.
func root(t *testing.T) string {
	t.Helper()
	r, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestGeneratedIsCurrent fails when a service's proxy, server or the
// catalog differ from what the generator writes. With
// FOUNDATION_GEN_UPDATE=1 it writes them instead.
func TestGeneratedIsCurrent(t *testing.T) {
	o := Options{Root: root(t)}
	if os.Getenv("FOUNDATION_GEN_UPDATE") == "1" {
		if _, err := Write(o); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := Check(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("generated services are stale; run `cli gen services` in backend (or FOUNDATION_GEN_UPDATE=1 go test ./features/services/gen):\n  %s", strings.Join(stale, "\n  "))
	}
}

func writeService(t *testing.T, body string) Options {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := filepath.Join(dir, "features", "x", "services", "xservice")
	if err := os.MkdirAll(svc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc, "Service.go"), []byte("package xservice\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Options{Root: dir}
}

func TestMarkersAndRefusals(t *testing.T) {
	o := writeService(t, `type Service interface {
	//svc:idempotent
	Get(args GetArgs) GetResponse
	//svc:local
	Poke(args PokeArgs)
	//svc:stream
	Watch(args WatchArgs) *WatchRequest
}
`)
	svcs, err := Read(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 || svcs[0].Name != "x" || len(svcs[0].Ops) != 3 {
		t.Fatalf("read %+v", svcs)
	}
	ops := svcs[0].Ops
	if !ops[0].Idempotent || !ops[1].Local || !ops[2].Stream || ops[2].Result != "*WatchRequest" {
		t.Fatalf("ops %+v", ops)
	}
	files, err := Generate(o)
	if err != nil {
		t.Fatal(err)
	}
	server := string(files[filepath.Join("features", "x", "services", "xservice", "server", "server.go")])
	if !strings.Contains(server, `"Get": services.Unary(svc.Get)`) || strings.Contains(server, "Poke") || strings.Contains(server, "Watch") {
		t.Fatalf("server:\n%s", server)
	}

	for _, bad := range []string{
		"type Service interface {\n\tPoke(args PokeArgs)\n}\n",
		"type Service interface {\n\tWatch(args WatchArgs) *WatchRequest\n}\n",
		"type Service interface {\n\tTwo(a, b int) R\n}\n",
	} {
		if _, err := Read(writeService(t, bad)); err == nil || !strings.Contains(err.Error(), "cannot travel") {
			t.Errorf("%q: err %v", bad, err)
		}
	}
}
