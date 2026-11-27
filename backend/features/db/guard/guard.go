// Package guard keeps storage with its owners (forge 21d-1,
// documentation/DataServices.md): only a data service's own packages may
// hold a database or JetStream handle. Check walks a module's packages
// and reports every one that imports a storage package without a row in
// the allow list, and every row whose package no longer does, so the list
// only shrinks.
package guard

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Storage is an import that opens storage: a database or JetStream.
type Storage struct {
	Import string
	Name   string
}

// Storages are the imports the guard watches.
var Storages = []Storage{
	{Import: "gorm.io/gorm", Name: "a database (*gorm.DB)"},
	{Import: "github.com/nats-io/nats.go/jetstream", Name: "JetStream streams and buckets"},
}

// Row allows one package to hold storage handles.
type Row struct {
	// Package is the module-relative directory, such as
	// "features/identity".
	Package string
	// Owner is the data service the storage belongs to.
	Owner string
	// Storage names what it holds.
	Storage string
	// Until is the sub-part that removes the row ("21d-2"), empty for a
	// package that owns its storage for good.
	Until string
	// Imports limits the row to some Storages (all when empty).
	Imports []string
}

func (r Row) allows(imp string) bool {
	if len(r.Imports) == 0 {
		return true
	}
	for _, i := range r.Imports {
		if i == imp {
			return true
		}
	}
	return false
}

// Check walks the Go packages under root's dirs (module-relative, such as
// "features" and "cli") and returns the problems: a package holding
// storage with no row (naming the owner to call instead), and a row whose
// package holds none of what it allows. Test files are skipped.
func Check(root string, dirs []string, allow []Row) ([]string, error) {
	rows := map[string]Row{}
	for _, r := range allow {
		rows[r.Package] = r
	}
	uses := map[string]map[string]bool{} // package → storage imports
	for _, d := range dirs {
		err := filepath.WalkDir(filepath.Join(root, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if name := e.Name(); name == "testdata" || name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
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
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			rel = filepath.ToSlash(rel)
			for _, is := range f.Imports {
				imp, _ := strconv.Unquote(is.Path.Value)
				for _, s := range Storages {
					if imp == s.Import {
						if uses[rel] == nil {
							uses[rel] = map[string]bool{}
						}
						uses[rel][imp] = true
					}
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	var problems []string
	for pkg, imps := range uses {
		for imp := range imps {
			r, ok := rows[pkg]
			if ok && r.allows(imp) {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s holds %s but owns none: call the owning data service's operations instead (documentation/DataServices.md), or, if it is a new data service, add its row to the guard's allow list", pkg, nameOf(imp)))
		}
	}
	for _, r := range allow {
		imps := uses[r.Package]
		held := false
		for imp := range imps {
			if r.allows(imp) {
				held = true
			}
		}
		if !held {
			problems = append(problems, fmt.Sprintf("%s holds no storage any more: remove its row (owner %s) from the guard's allow list", r.Package, r.Owner))
		}
	}
	sort.Strings(problems)
	return problems, nil
}

func nameOf(imp string) string {
	for _, s := range Storages {
		if s.Import == imp {
			return s.Name
		}
	}
	return imp
}
