package wiring

import (
	"testing"

	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Every registered feature contributes into a registry without a server, as
// TestMetadata's and the forge tools' catalog do, and its adapter types each
// come with an implementation.
func TestEveryFeatureContributesWithAnEmptyEnv(t *testing.T) {
	if len(Features()) == 0 {
		t.Fatal("no features registered")
	}
	reg := extensions.NewRegistry()
	if err := ContributeAll(reg, nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.Points()) == 0 {
		t.Fatal("the features contributed nothing")
	}
	var o adapter.Options
	Adapters(&o)
	if len(o.Types) == 0 || len(o.Impls) == 0 {
		t.Fatalf("adapters: %d types, %d impls", len(o.Types), len(o.Impls))
	}
}

func TestFeaturesAreInNameOrder(t *testing.T) {
	fs := Features()
	for i := 1; i < len(fs); i++ {
		if fs[i-1].Name >= fs[i].Name {
			t.Fatalf("%s before %s", fs[i-1].Name, fs[i].Name)
		}
	}
}
