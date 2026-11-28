package images_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/images"
	"github.com/SgtFlame/foundation/backend/features/ai/providers/gemini"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// The tool reaches image.google over the connector bus, as every
// connector call does, and gives the stored files to the model.
func TestToolOverTheBus(t *testing.T) {
	reg := extensions.NewRegistry()
	reg.Declare(connectors.Contracts, connectors.Types, cognition.Nodes, tools.Tools)
	images.Contribute(reg)
	var cx connectors.Deps
	images.ContributeNodes(reg, func() connectors.Deps { return cx })

	srv := natsd.StartTest(t)
	r := connectors.NewRegistry(reg, srv.JS())
	t.Cleanup(r.Close)
	cx = connectors.Deps{Registry: r, Dispatcher: &connectors.Dispatcher{NC: srv.Conn(), Registry: r, Timeout: 5 * time.Second}}

	stand, _ := standIn(t, "generate.json")
	files := &memFiles{files: map[string]memFile{}}
	deps := images.Deps{Images: gemini.New(gemini.WithAPIKey("test"), gemini.WithBaseURL(stand.URL)), Files: files}
	nc, err := srv.Connect("adapter")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = adapter.Serve(ctx, nc, adapter.Options{Name: "images", Types: []connectors.Type{images.Type()}, Contracts: []connectors.Contract{images.Contract()},
			Impls: []adapter.Impl{images.Impl(func() images.Deps { return deps })}})
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done; nc.Close() })

	var tool tools.Tool
	for _, x := range tools.Tools.All(reg) {
		if x.ID == images.ToolID {
			tool = x
		}
	}
	if tool.Effect != tools.EffectWrite {
		t.Fatalf("the tool's effect is %q", tool.Effect)
	}
	call := tools.Call{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: 1}},
		Name: images.ToolID, Args: json.RawMessage(`{"prompt":"A map of the Northern Isles","style":"ink sketch"}`)}
	var res tools.Result
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err = tool.Run(call)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond) // until the adapter is online
	}
	if err != nil {
		t.Fatal(err)
	}
	list, _ := res.Content["images"].([]any)
	if len(list) != 1 || res.Content["model"] != "gemini-2.5-flash-image" || len(files.files) != 1 {
		t.Fatalf("result %+v, files %d", res, len(files.files))
	}
}
