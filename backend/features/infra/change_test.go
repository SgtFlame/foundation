package infra_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/infra"
)

// The chat's change: several edits as one batch, the draft's changes since
// its head and the plan of the draft; a refused edit changes nothing
// (forge 22q).
func TestChangeDraft(t *testing.T) {
	st := newStack(t)
	seedDraft(st)
	st.must("infra.config.put", m{"kind": "profiles", "item": m{"id": "runner", "name": "Runner", "kind": "container"}})
	st.must("infra.config.put", m{"kind": "placements", "item": m{"id": "runner", "profile": "runner", "sites": []m{{"site": "west", "count": 2}}}})
	c := st.config()
	st.must("infra.config.commit", m{"message": "Two runners", "expect": c.Rev})
	sess := openTestServer(t)
	deps := infra.Deps{Bus: st.bus, Store: st.p, Compute: testCompute{sess}}
	item, _ := json.Marshal(m{"id": "runner", "profile": "runner", "sites": []m{{"site": "west", "count": 3}}})
	r, err := infra.ChangeDraft(operator(7), st.reg, deps, "", []infra.ChangeOp{{Op: "put", Kind: "placements", Item: item}}, "round-1", "chat", true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request != "round-1" || len(r.Changes) != 1 || !strings.Contains(r.Changes[0].Summary, "runner") {
		t.Fatalf("changes = %+v", r)
	}
	if c := st.config(); c.Draft.Placements[0].Sites[0].Count != 3 || c.Head != 1 {
		t.Fatalf("draft = %+v", c)
	}
	third := false
	for _, s := range r.Plan.Steps {
		if s.ID == "instance:west/runner/runner/3" && s.Op == infra.OpCreate {
			third = true
		}
	}
	if r.Plan == nil || !third {
		t.Fatalf("plan = %+v", r.Plan)
	}
	// One refused op refuses the batch.
	bad, _ := json.Marshal(m{"id": "east", "site": "east", "size": "m", "count": 1})
	if _, err := infra.ChangeDraft(operator(7), st.reg, deps, "", []infra.ChangeOp{{Op: "remove", Kind: "placements", ID: "runner"}, {Op: "put", Kind: "hostPools", Item: bad}}, "", "chat", false); err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("refused batch: %v", err)
	}
	if c := st.config(); len(c.Draft.Placements) != 1 {
		t.Fatalf("the refused batch changed the draft: %+v", c.Draft.Placements)
	}
}
