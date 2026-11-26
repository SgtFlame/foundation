package features

import (
	"encoding/json"
	"slices"
	"testing"
)

// Forge 7p-3: an agent run's reach survives being stored as a reference
// (a job it enqueues) and reads back as keys.
func TestPrincipalReach(t *testing.T) {
	p := AgentPrincipal(1, 7, 2)
	p.Reach = "mailThread:t1 mailThread:t2"
	raw, err := json.Marshal(p.Ref())
	if err != nil {
		t.Fatal(err)
	}
	var r PrincipalRef
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if back := r.In(1); back != p {
		t.Errorf("round trip: %+v, want %+v", back, p)
	}
	if !slices.Equal(p.ReachKeys(), []string{"mailThread:t1", "mailThread:t2"}) || len(AgentPrincipal(1, 7, 2).ReachKeys()) != 0 {
		t.Errorf("keys: %v", p.ReachKeys())
	}
}
