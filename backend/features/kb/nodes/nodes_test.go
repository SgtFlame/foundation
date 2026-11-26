package kbnodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	kbnodes "github.com/SgtFlame/foundation/backend/features/kb/nodes"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
)

// collab records what EditDoc was asked and applies it to text.
type collab struct {
	collabservice.Service
	text  string
	asked collabservice.EditDocArgs
}

func (c *collab) EditDoc(args collabservice.EditDocArgs) collabservice.EditDocResponse {
	c.asked = args
	p := collabservice.Prepare(c.text, args.Edits)
	for i, ok := range p.Applied {
		if !ok && !args.Each {
			return collabservice.EditDocResponse{Common: *args.Fail(http.StatusConflict, errors.New(p.Problems[i]))}
		}
	}
	c.text = p.Text
	return collabservice.EditDocResponse{Common: features.OK(), Applied: p.Applied, Problems: p.Problems}
}

func TestEditTogether(t *testing.T) {
	reg := extensions.NewRegistry()
	svc := &collab{text: "A table holds four."}
	kbnodes.Contribute(reg, func() collabservice.Service { return svc })
	var nt cognition.NodeType
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == kbnodes.EditID {
			nt = n
		}
	}
	if problems := cognition.MetaProblems(nt); len(problems) > 0 {
		t.Fatalf("Edit text together lacks %v", problems)
	}
	run := func(config string, in cognition.Values) (cognition.Values, string) {
		x := &cognition.Exec{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, Node: cognition.Node{ID: "n", Type: kbnodes.EditID, Config: json.RawMessage(config)}}
		out, pin, err := nt.Run(x, in)
		if err != nil {
			t.Fatal(err)
		}
		return out, pin
	}
	out, pin := run(`{"entity":"reqDraft","field":"markdown","each":true}`, cognition.Values{"id": "d1",
		"edits": []any{map[string]any{"start": 14, "end": 18, "find": "four", "replace": "six"}, map[string]any{"find": "lunch", "replace": "x"}}})
	if pin != cognition.Then || svc.text != "A table holds six." {
		t.Fatalf("pin %q, text %q, out %v", pin, svc.text, out)
	}
	var ref struct{ Entity, ID, Field string }
	_ = json.Unmarshal(svc.asked.Ref, &ref)
	if svc.asked.Kind != "record" || ref.Entity != "reqDraft" || ref.ID != "d1" || ref.Field != "markdown" || !svc.asked.Each {
		t.Fatalf("asked %+v %+v", svc.asked, ref)
	}
	if applied := out["applied"].([]any); applied[0] != true || applied[1] != false || out["problems"].([]any)[1] == "" {
		t.Fatalf("out = %v", out)
	}
	// The pins win over the config; one that cannot apply fails them all
	if _, pin := run(`{"entity":"x","field":"y"}`, cognition.Values{"entity": "reqDraft", "field": "markdown", "id": "d1", "edits": []any{map[string]any{"find": "lunch"}}}); pin != "failed" || ref.Entity != "reqDraft" {
		t.Fatalf("pin = %q", pin)
	}
	if out, pin := run(`{}`, cognition.Values{"id": "d1", "edits": "nonsense"}); pin != "failed" || out["error"] == "" {
		t.Fatalf("bad edits gave %q %v", pin, out)
	}
}
