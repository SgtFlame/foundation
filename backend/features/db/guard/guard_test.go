package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStorageStaysWithItsOwners is the guard over Foundation itself.
func TestStorageStaysWithItsOwners(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := Check(root, []string{"features", "cli", "cmd"}, Allow)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("storage outside its owners (documentation/DataServices.md):\n  %s", strings.Join(problems, "\n  "))
	}
}

func TestGuardRefusesANewHandle(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("features/owner/a.go", "package owner\nimport _ \"gorm.io/gorm\"\n")
	write("features/sneaky/b.go", "package sneaky\nimport \"gorm.io/gorm\"\nvar DB *gorm.DB\n")
	write("features/sneaky/b_test.go", "package sneaky\nimport _ \"github.com/nats-io/nats.go/jetstream\"\n")
	write("features/gone/c.go", "package gone\n")
	allow := []Row{
		{Package: "features/owner", Owner: "owner"},
		{Package: "features/gone", Owner: "gone", Until: "21d-2"},
	}
	problems, err := Check(root, []string{"features"}, allow)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 || !strings.HasPrefix(problems[0], "features/gone holds no storage") || !strings.HasPrefix(problems[1], "features/sneaky holds a database") {
		t.Fatalf("problems %q", problems)
	}
}
