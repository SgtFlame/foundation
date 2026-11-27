// Package wiring is where a feature wires itself into the server, so that a
// new feature adds its own file here instead of editing a shared list in
// serve.go, the metadata test's registry or the forge tools' catalog (the
// user, 2026-11-27: parallel pull requests kept conflicting at the end of
// those lists).
//
// A feature's file (wiring/<feature>.go) registers it from init:
//
//	func init() {
//		Register(Feature{
//			Name: "calendar",
//			Contribute: func(reg *extensions.Registry, env *Env) error {
//				calendar.Contribute(reg)
//				return nil
//			},
//			Adapter: func(o *adapter.Options) { ... },
//		})
//	}
//
// Foundation's and forge's serve call ContributeAll after the built-in
// contributions and Adapters in connectorImpls; registries without a server
// (apps/tools' TestMetadata, forge's devtools catalog) call ContributeAll with
// an empty Env. A forge feature registers the same way from a forge package
// that forge's serve imports.
//
// Only contributions that do not depend on their order among other features
// belong here: a contribution that must come before or after a particular
// built-in one (conversationsettings, livetools) stays in serve.go.
package wiring

import (
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connections"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	recordsfiles "github.com/SgtFlame/foundation/backend/features/records/files"
)

// Feature is one feature's wiring.
type Feature struct {
	// Name identifies the feature; features are contributed in name order.
	Name string
	// Contribute registers the feature's contributions. Read env only inside
	// the functions handed to contributions (late-bound): serve fills it in
	// after the registry is built, and registries without a server leave it
	// empty.
	Contribute func(reg *extensions.Registry, env *Env) error
	// Adapter adds the connector types, contracts and implementations that
	// the server's in-process adapter serves for the feature (optional).
	Adapter func(o *adapter.Options)
}

// Env is what a feature's contributions may read from the server. Its fields
// point at serve's own variables, so they hold their values once serve has
// set them; every accessor is safe on an empty Env.
type Env struct {
	ConnectorDeps  *connectors.Deps
	ConnectionDeps *connections.Deps
	// Files is the record files store, set once it is made (files a node
	// reads, such as an uploaded .ics or transcript).
	Files *recordsfiles.Files
}

// Connectors is the connectors' deps, or empty ones.
func (e *Env) Connectors() connectors.Deps {
	if e == nil || e.ConnectorDeps == nil {
		return connectors.Deps{}
	}
	return *e.ConnectorDeps
}

// Connections is the connections' deps, or empty ones.
func (e *Env) Connections() connections.Deps {
	if e == nil || e.ConnectionDeps == nil {
		return connections.Deps{}
	}
	return *e.ConnectionDeps
}

// RecordFiles is the record files store, or nil before it is made.
func (e *Env) RecordFiles() recordsfiles.Files {
	if e == nil || e.Files == nil {
		return nil
	}
	return *e.Files
}

// Executable is this binary's path, for features that parse in a child
// process of it; "" when it cannot be found.
func (e *Env) Executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

var (
	mu       sync.Mutex
	features []Feature
)

// Register adds a feature; call it from init in the feature's own file. A
// second feature with the same name panics.
func Register(f Feature) {
	mu.Lock()
	defer mu.Unlock()
	if f.Name == "" {
		panic("wiring: a feature needs a name")
	}
	for _, g := range features {
		if g.Name == f.Name {
			panic("wiring: feature " + f.Name + " registered twice")
		}
	}
	features = append(features, f)
}

// Features is every registered feature, in name order.
func Features() []Feature {
	mu.Lock()
	defer mu.Unlock()
	out := slices.Clone(features)
	slices.SortFunc(out, func(a, b Feature) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// ContributeAll contributes every registered feature, in name order.
func ContributeAll(reg *extensions.Registry, env *Env) error {
	if env == nil {
		env = &Env{}
	}
	for _, f := range Features() {
		if f.Contribute == nil {
			continue
		}
		if err := f.Contribute(reg, env); err != nil {
			return err
		}
	}
	return nil
}

// Adapters adds every registered feature's in-process adapter types to o.
func Adapters(o *adapter.Options) {
	for _, f := range Features() {
		if f.Adapter != nil {
			f.Adapter(o)
		}
	}
}
