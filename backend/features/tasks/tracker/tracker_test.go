package tracker_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/tasks/tracker"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, got, "", "  "); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	buf.WriteByte('\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", name, err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("%s differs from its golden file (run with -update to accept):\n%s", name, buf.String())
	}
}

// tasks.tracker@1 passes the contract check, and its operations map to
// the tool shapes of its golden files.
func TestTrackerContract(t *testing.T) {
	c := tracker.Contract()
	if ps := connectors.CheckContract(c); len(ps) > 0 {
		t.Fatal(ps)
	}
	for _, op := range c.Operations {
		spec, _ := json.Marshal(connectors.ToolSpec(c, op))
		golden(t, op.ID+".toolspec.json", spec)
		golden(t, op.ID+".mcp.json", connectors.MCPTool(c, op))
		for _, vendor := range []string{connectors.VendorGemini, connectors.VendorAnthropic, connectors.VendorOpenAI} {
			f, err := connectors.FunctionSpec(c, op, vendor)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, op.ID+"."+vendor+".json", f)
		}
	}
}
