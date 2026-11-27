package packages

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/controlplane/natsauth"
	"github.com/nats-io/nats.go/jetstream"
)

func write(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPublishAndServe: a published repository is served byte for byte by
// an instance's daemon user; a publish that fails before its index is
// switched leaves the old index served whole; old generations are dropped.
func TestPublishAndServe(t *testing.T) {
	ctx := context.Background()
	s := natsauth.StartTestSecure(t)
	cp, _ := s.Conn(natsauth.Control, "cli", natsauth.Everything)
	defer cp.Close()
	cpJS, _ := jetstream.New(cp)
	dc, err := s.Conn(natsauth.Control, "axiomd-inst.n1", ctl.DaemonPermissions(ctl.Target{Kind: ctl.KindInstance, ID: "n1"}))
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	dJS, _ := jetstream.New(dc)
	srv := httptest.NewServer(&Server{JS: dJS})
	defer srv.Close()
	get := func(p string) (int, []byte) {
		t.Helper()
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	deb := make([]byte, 700_000) // several chunks
	_, _ = rand.Read(deb)
	dir := t.TempDir()
	write(t, dir, "pool/main/axiomd_1.0_amd64.deb", deb)
	write(t, dir, "dists/stable/InRelease", []byte("index 1"))
	write(t, dir, "dists/stable/main/binary-amd64/Packages", []byte("Package: axiomd\nVersion: 1.0\n"))
	p, err := Publish(ctx, cpJS, dir)
	if err != nil || len(p.Uploaded) != 3 || p.Channels["stable"]["axiomd"] != "1.0" {
		t.Fatalf("publish %+v %v", p, err)
	}
	if code, b := get("/pool/main/axiomd_1.0_amd64.deb"); code != 200 || !bytes.Equal(b, deb) {
		t.Fatalf("the package: %d, %d bytes", code, len(b))
	}
	if code, b := get("/dists/stable/InRelease"); code != 200 || string(b) != "index 1" {
		t.Fatalf("the index: %d %q", code, b)
	}
	if code, _ := get("/dists/../../current"); code != 404 {
		t.Fatalf("a path outside: %d", code)
	}
	if code, _ := get("/healthz"); code != 200 {
		t.Fatal("healthz")
	}

	// A publish that dies before switching: the old index, whole
	write(t, dir, "pool/main/axiomd_1.1_amd64.deb", []byte("new"))
	write(t, dir, "dists/stable/InRelease", []byte("index 2"))
	write(t, dir, "dists/stable/main/binary-amd64/Packages", []byte("Package: axiomd\nVersion: 1.1\n"))
	beforePut = func(name string) error {
		if name == Current {
			return errors.New("the uploader died")
		}
		return nil
	}
	if _, err := Publish(ctx, cpJS, dir); err == nil {
		t.Fatal("the failed publish succeeded")
	}
	beforePut = nil
	if _, b := get("/dists/stable/InRelease"); string(b) != "index 1" {
		t.Fatalf("a half publish is served: %q", b)
	}
	if _, b := get("/dists/stable/main/binary-amd64/Packages"); !bytes.Contains(b, []byte("1.0")) {
		t.Fatalf("a half publish is served: %q", b)
	}
	// Published again: the pool file already there is kept, the new index served
	p, err = Publish(ctx, cpJS, dir)
	if err != nil || p.Kept != 2 {
		t.Fatalf("publish %+v %v", p, err)
	}
	if _, b := get("/dists/stable/InRelease"); string(b) != "index 2" {
		t.Fatalf("the new index: %q", b)
	}
	cpStore, _ := cpJS.ObjectStore(ctx, ctl.RepoBucket)
	if gen, ch, err := Channels(ctx, cpStore); err != nil || gen != p.Generation || ch["stable"]["axiomd"] != "1.1" {
		t.Fatalf("channels %s %v %v", gen, ch, err)
	}
	for i := 0; i < 3; i++ {
		if _, err := Publish(ctx, cpJS, dir); err != nil {
			t.Fatal(err)
		}
	}
	store, _ := cpJS.ObjectStore(ctx, ctl.RepoBucket)
	list, _ := store.List(ctx)
	gens := map[string]bool{}
	for _, o := range list {
		if len(o.Name) > 4 && o.Name[:4] == "gen/" {
			gens[o.Name[4:30]] = true
		}
	}
	if len(gens) != KeepGenerations {
		t.Fatalf("%d generations kept", len(gens))
	}
	// The daemon's user only reads
	dstore, err := dJS.ObjectStore(ctx, ctl.RepoBucket)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 2*time.Second) // a refused publish answers nothing
	defer cancel()
	if _, err := dstore.PutString(short, Current, "evil"); err == nil {
		t.Fatal("a daemon wrote the repository")
	}
}
