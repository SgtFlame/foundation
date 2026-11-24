package kbtools

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// recorder is a retrieval service that keeps what it was asked.
type recorder struct {
	retrievalservice.Service
	got []retrievalservice.RetrieveArgs
}

func (r *recorder) Retrieve(args retrievalservice.RetrieveArgs) retrievalservice.RetrieveResponse {
	r.got = append(r.got, args)
	return retrievalservice.RetrieveResponse{Common: features.OK()}
}

func searchTool(t *testing.T, r *recorder) tools.Tool {
	reg := extensions.NewRegistry()
	reg.Declare(tools.Tools)
	Contribute(reg, func() Deps { return Deps{Retrieval: r} })
	for _, tool := range tools.Tools.All(reg) {
		if tool.ID == "kb.search" {
			return tool
		}
	}
	t.Fatal("no kb.search")
	return tools.Tool{}
}

// kb.search takes how it searches from the call's context, never from the
// model (forge 15a): without options it searches the Volumes alone with the
// dials at 0, as it always has; with them, their sources, dials, Top-K and
// Volumes.
func TestSearchOptions(t *testing.T) {
	r := &recorder{}
	tool := searchTool(t, r)
	run := func(ctx context.Context, p features.Principal, args string) error {
		_, err := tool.Run(tools.Call{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: p}, Name: "kb.search", Args: json.RawMessage(args)})
		return err
	}
	local := features.LocalPrincipal()
	if err := run(context.Background(), local, `{"query":"refunds"}`); err != nil {
		t.Fatal(err)
	}
	got := r.got[0]
	if !slices.Equal(got.Sources, []string{"library"}) || got.TopK != 6 || got.LexicalSemantic != 0 || got.ChronicleLibrary != 0 || got.VolumeIDs != nil {
		t.Errorf("without options: %+v", got)
	}
	if err := run(context.Background(), local, `{"query":"refunds","count":3}`); err != nil || r.got[1].TopK != 3 {
		t.Errorf("the model's count: %+v %v", r.got[1], err)
	}

	o := retrieval.Options{Sources: []string{"library", "chronicle"}, LexicalSemantic: 3, ChronicleLibrary: -2, TopK: 4, VolumeIDs: []uint64{7}}
	if err := run(retrieval.WithOptions(context.Background(), o), local, `{"query":"refunds","count":12}`); err != nil {
		t.Fatal(err)
	}
	got = r.got[2]
	if !slices.Equal(got.Sources, o.Sources) || got.TopK != 4 || got.LexicalSemantic != 3 || got.ChronicleLibrary != -2 || !slices.Equal(got.VolumeIDs, []uint64{7}) {
		t.Errorf("with options: %+v", got)
	}

	// Outside the default tenant: memory may be searched, Volumes alone fail
	other := features.Principal{Kind: features.PrincipalAgent, AgentID: 5, TenantID: 9}
	if err := run(context.Background(), other, `{"query":"refunds"}`); err == nil {
		t.Error("Volumes searched outside the default tenant")
	}
	if err := run(retrieval.WithOptions(context.Background(), retrieval.Options{Sources: []string{"chronicle"}}), other, `{"query":"refunds"}`); err != nil {
		t.Errorf("memory outside the default tenant: %v", err)
	}
}

// ParseOptions reads what a graph passes, ids as text.
func TestParseOptions(t *testing.T) {
	o, set, err := retrieval.ParseOptions(map[string]any{"sources": []any{"chronicle"}, "topK": 5, "volumeIds": []any{"7", 8}})
	if err != nil || !set || !slices.Equal(o.VolumeIDs, []uint64{7, 8}) || o.TopK != 5 || !slices.Equal(o.Sources, []string{"chronicle"}) {
		t.Errorf("%+v %v %v", o, set, err)
	}
	if _, set, _ := retrieval.ParseOptions(nil); set {
		t.Error("nil is no options")
	}
	if _, _, err := retrieval.ParseOptions(map[string]any{"lexicalSemantic": 9}); err == nil {
		t.Error("a dial past 5")
	}
}
