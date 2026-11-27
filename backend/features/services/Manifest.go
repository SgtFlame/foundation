package services

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Manifest is what a service declares about itself (forge 21f-1), in its
// package's Manifest.go as `var Manifest = services.Manifest{…}`; the
// generated catalog carries it on the service's Spec.
type Manifest struct {
	Name string // the wire name; the Spec's when empty
	// Calls are the services whose proxies its code takes, by wire name.
	Calls []string
	// Owns are the storages it holds (documentation/DataServices.md's
	// inventory), and Subjects the NATS subjects owning them needs
	// (streams, buckets: "$KV.cx_>"), beyond its own svc.<name>.>.
	Owns     []string
	Subjects []string
	// Secrets are the Vault paths it reads, Config the settings sections.
	Secrets []string
	Config  []string
	// Publishes and Follows are the event subjects or buses it writes and
	// reads.
	Publishes []string
	Follows   []string
}

// Permissions are a process's NATS subjects.
type Permissions struct {
	Publish   []string `json:"publish"`
	Subscribe []string `json:"subscribe"`
}

// ProcessPermissions are the NATS permissions of a process running set
// (by wire name) from the services' manifests: its own services' calls
// (svc.<own>.>, with their cancel and wait subjects) and the subjects of
// what they own and follow; the services they call and reply inboxes.
func ProcessPermissions(specs []Spec, set []string) Permissions {
	p := Permissions{Publish: []string{"_INBOX.>"}, Subscribe: []string{"_INBOX.>"}}
	for _, s := range specs {
		if !slices.Contains(set, s.Name) {
			continue
		}
		m := s.Manifest
		p.Subscribe = append(p.Subscribe, "svc."+s.Name+".>")
		for _, c := range m.Calls {
			p.Publish = append(p.Publish, "svc."+c+".>")
		}
		p.Publish = append(p.Publish, m.Subjects...)
		p.Subscribe = append(p.Subscribe, m.Subjects...)
		p.Publish = append(p.Publish, m.Publishes...)
		p.Subscribe = append(p.Subscribe, m.Follows...)
	}
	sort.Strings(p.Publish)
	sort.Strings(p.Subscribe)
	p.Publish, p.Subscribe = slices.Compact(p.Publish), slices.Compact(p.Subscribe)
	return p
}

// CheckCalls reports the proxies a service package's code imports (other
// than its own) that its manifest does not declare in Calls: a package
// importing <x>service/proxy calls x. dir is the package's directory,
// module its module path.
func CheckCalls(dir, module string, m Manifest) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if base := d.Name(); path != dir && (base == "proxy" || base == "server" || base == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if !strings.HasPrefix(p, module+"/") || !strings.HasSuffix(p, "service/proxy") {
				continue
			}
			pkg := filepath.Base(filepath.Dir(p))
			name := strings.TrimSuffix(pkg, "service")
			if name == m.Name || slices.Contains(m.Calls, name) {
				continue
			}
			rel, _ := filepath.Rel(dir, path)
			problems = append(problems, fmt.Sprintf("%s: %s calls the %s service through its proxy, which its Manifest's Calls does not declare", rel, m.Name, name))
		}
		return nil
	})
	return problems, err
}

// Named is m with its wire name, for the generated catalog.
func Named(m Manifest, name string) Manifest {
	if m.Name == "" {
		m.Name = name
	}
	return m
}
