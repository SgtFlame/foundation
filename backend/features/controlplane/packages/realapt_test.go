package packages

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/live/natsd"
)

// TestRealApt: a repository scripts/repo.sh built, published and served
// here, is accepted by the real apt-get, which downloads a package from
// it. It runs only with AXIOM_REPO_DIR (the repository) and AXIOM_REPO_KEY
// (its public key, repo-signing.asc) set, and apt-get on the PATH:
//
//	scripts/package.sh 0.1.0 && scripts/repo-key.sh /tmp/k &&
//	scripts/repo.sh /tmp/repo stable --key-file /tmp/k/repo-signing.key out/packages/*.deb &&
//	AXIOM_REPO_DIR=/tmp/repo AXIOM_REPO_KEY=/tmp/k/repo-signing.asc go test -run TestRealApt ./features/controlplane/packages
func TestRealApt(t *testing.T) {
	dir, key := os.Getenv("AXIOM_REPO_DIR"), os.Getenv("AXIOM_REPO_KEY")
	if dir == "" || key == "" {
		t.Skip("AXIOM_REPO_DIR and AXIOM_REPO_KEY are not set")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("no apt-get")
	}
	srv := natsd.StartTest(t)
	if _, err := Publish(context.Background(), srv.JS(), dir); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(&Server{JS: srv.JS()})
	defer h.Close()
	a := t.TempDir()
	for _, d := range []string{"etc/sources.list.d", "etc/preferences.d", "state/lists/partial", "cache/archives/partial", "download"} {
		_ = os.MkdirAll(filepath.Join(a, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(a, "etc/sources.list"), nil, 0o644)
	src := "Types: deb\nURIs: " + h.URL + "\nSuites: stable\nComponents: main\nSigned-By: " + key + "\n"
	_ = os.WriteFile(filepath.Join(a, "etc/sources.list.d/axiom-first-party.sources"), []byte(src), 0o644)
	opts := []string{
		"-o", "Dir::Etc::sourcelist=" + filepath.Join(a, "etc/sources.list"),
		"-o", "Dir::Etc::sourceparts=" + filepath.Join(a, "etc/sources.list.d"),
		"-o", "Dir::Etc::preferencesparts=" + filepath.Join(a, "etc/preferences.d"),
		"-o", "Dir::State=" + filepath.Join(a, "state"),
		"-o", "Dir::Cache=" + filepath.Join(a, "cache"),
		"-o", "Debug::NoLocking=1",
		"-o", "APT::Sandbox::User=root",
		"-o", "APT::Get::List-Cleanup=0",
	}
	out, err := exec.Command("apt-get", append(opts, "update")...).CombinedOutput()
	if err != nil || strings.Contains(string(out), "W:") || strings.Contains(string(out), "E:") {
		t.Fatalf("apt-get update: %v\n%s", err, out)
	}
	cmd := exec.Command("apt-get", append(opts, "download", "axiomd")...)
	cmd.Dir = filepath.Join(a, "download")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apt-get download: %v\n%s", err, out)
	}
	if m, _ := filepath.Glob(filepath.Join(a, "download", "axiomd_*.deb")); len(m) != 1 {
		t.Fatal("no package downloaded")
	}
}
