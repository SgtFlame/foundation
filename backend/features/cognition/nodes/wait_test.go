package cognitionnodes

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Wait (forge 07i) pauses for its seconds, the input's over the config's,
// and ends with the run's context.
func TestWait(t *testing.T) {
	reg := extensions.NewRegistry()
	Contribute(reg)
	var nt cognition.NodeType
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == Wait {
			nt = n
		}
	}
	if nt.Run == nil {
		t.Fatal("no Wait node")
	}
	run := func(ctx context.Context, config string, in cognition.Values) (string, time.Duration, error) {
		x := cognition.NewExec(features.ServiceArgs{Ctx: ctx}, 1, cognition.Node{ID: "w", Type: Wait, Config: json.RawMessage(config)}, cognition.Values{})
		start := time.Now()
		_, pin, err := nt.Run(x, in)
		return pin, time.Since(start), err
	}
	if pin, d, err := run(context.Background(), `{"seconds":0.05}`, cognition.Values{}); err != nil || pin != cognition.Then || d < 50*time.Millisecond {
		t.Errorf("config: %s after %s, %v", pin, d, err)
	}
	if pin, d, err := run(context.Background(), `{"seconds":10}`, cognition.Values{"seconds": 0.02}); err != nil || pin != cognition.Then || d > 2*time.Second {
		t.Errorf("input over config: %s after %s, %v", pin, d, err)
	}
	if _, d, err := run(context.Background(), `{}`, cognition.Values{"seconds": -1}); err != nil || d > 50*time.Millisecond {
		t.Errorf("negative: after %s, %v", d, err)
	}
	if _, _, err := run(context.Background(), `{}`, cognition.Values{"seconds": "soon"}); err == nil {
		t.Error("text seconds accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, d, err := run(ctx, `{"seconds":60}`, cognition.Values{}); err == nil || d > 2*time.Second {
		t.Errorf("cancelled: after %s, %v", d, err)
	}
}
