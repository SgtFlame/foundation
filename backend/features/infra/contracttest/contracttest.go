// Package contracttest is the suite every infra.compute@1 type passes
// (forge 22c): the Infrastructure test server here, each provider type
// against recorded responses in 22g and 22l to 22n.
package contracttest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/infra"
)

// Caller calls one infra.compute operation of the type under test.
type Caller func(op string, args any) (json.RawMessage, error)

// Options say what the suite may use: a size and region the type has,
// and how long a created host may take to be running.
type Options struct {
	Size, Region string
	Ready        time.Duration
}

// Run runs the suite: create is keyed, list finds what create made, get
// reads it, delete is idempotent and leaves it gone, sizes with vms are
// marked, and every result has the contract's required fields.
func Run(t *testing.T, call Caller, o Options) {
	t.Helper()
	if o.Ready == 0 {
		o.Ready = 5 * time.Second
	}
	must := func(op string, args any, into any) {
		t.Helper()
		raw, err := call(op, args)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if into != nil {
			if err := json.Unmarshal(raw, into); err != nil {
				t.Fatalf("%s: %v in %s", op, err, raw)
			}
		}
	}
	if _, err := call("check", map[string]any{}); err != nil {
		t.Fatalf("check: %v", err)
	}
	var sizes struct{ Sizes []infra.ProviderSize }
	must("size.list", map[string]any{}, &sizes)
	found := false
	for _, s := range sizes.Sizes {
		if s.ID == "" {
			t.Errorf("a size without an id: %+v", s)
		}
		found = found || s.ID == o.Size
	}
	if !found {
		t.Fatalf("size.list has no %q: %+v", o.Size, sizes.Sizes)
	}
	var regions struct{ Regions []struct{ ID string } }
	must("region.list", map[string]any{}, &regions)
	if len(regions.Regions) == 0 {
		t.Fatal("region.list is empty")
	}

	id := "contract-suite-1"
	var a, b infra.ProviderHost
	must("host.create", map[string]any{"id": id, "size": o.Size, "region": o.Region}, &a)
	if a.ID != id || a.ProviderID == "" || a.State == "" {
		t.Fatalf("host.create gave %+v", a)
	}
	must("host.create", map[string]any{"id": id, "size": o.Size, "region": o.Region}, &b)
	if b.ProviderID != a.ProviderID {
		t.Fatalf("a second create made another host: %s then %s", a.ProviderID, b.ProviderID)
	}
	var list struct{ Hosts []infra.ProviderHost }
	must("host.list", map[string]any{}, &list)
	n := 0
	for _, h := range list.Hosts {
		if h.ID == id {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("host.list has %d hosts %s: %+v", n, id, list.Hosts)
	}
	deadline := time.Now().Add(o.Ready)
	for {
		var h infra.ProviderHost
		must("host.get", map[string]any{"id": id}, &h)
		if h.State == infra.HostRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host %s is %s after %s", id, h.State, o.Ready)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var del struct{ Deleted bool }
	must("host.delete", map[string]any{"id": id}, &del)
	if !del.Deleted {
		t.Fatal("host.delete deleted nothing")
	}
	must("host.delete", map[string]any{"id": id}, &del)
	if del.Deleted {
		t.Fatal("a second delete deleted something")
	}
	_, err := call("host.get", map[string]any{"id": id})
	var ce *connectors.Error
	if !errors.As(err, &ce) || ce.Code != connectors.CodeNotFound {
		t.Fatalf("host.get after delete = %v, want notFound", err)
	}
}
