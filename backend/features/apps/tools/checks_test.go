package apptools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// A check of other apps adds its warnings to forge.check's, prefixed with
// its id; a failing check and a tool that does not check add nothing.
func TestAppChecks(t *testing.T) {
	reg := extensions.NewRegistry()
	reg.Declare(tools.Tools, tools.Sources)
	var got map[string]string
	tools.Sources.Register(reg, tools.Source{Descriptor: extensions.Descriptor{ID: "test", Source: "test"}, Tools: func(features.ServiceArgs) []tools.Tool {
		return []tools.Tool{
			{Descriptor: extensions.Descriptor{ID: "requirements.gaps"}, Checks: true, Run: func(c tools.Call) (tools.Result, error) {
				_ = json.Unmarshal(c.Args, &got)
				return tools.Result{Content: map[string]any{"warnings": []any{
					map[string]any{"path": "board:po/Refunds", "message": "no part serves Refunds", "fix": "link a part"},
					map[string]any{"path": "x"},
				}}}, nil
			}},
			{Descriptor: extensions.Descriptor{ID: "other.fails"}, Checks: true, Run: func(tools.Call) (tools.Result, error) {
				return tools.Result{}, context.Canceled
			}},
			{Descriptor: extensions.Descriptor{ID: "other.plain"}, Run: func(tools.Call) (tools.Result, error) {
				t.Fatal("a tool without checks ran")
				return tools.Result{}, nil
			}},
		}
	}})
	call := tools.Call{ServiceArgs: features.BackgroundArgs(context.Background())}
	ws := AppChecks(call, reg, "tasks", "b")
	if got["appId"] != "tasks" || got["branch"] != "b" {
		t.Fatalf("the check's args: %v", got)
	}
	if len(ws) != 1 || ws[0].Path != "requirements.gaps:board:po/Refunds" || ws[0].Code != "warning" || ws[0].Fix != "link a part" {
		t.Fatalf("warnings: %+v", ws)
	}
}
