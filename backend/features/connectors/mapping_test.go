package connectors_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares got with testdata/golden/<name>, rewriting it with
// -update.
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

// compile compiles a schema file of testdata, with ref inside it.
func compile(t *testing.T, file, ref string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	url := "file:///" + file
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(url + ref)
	if err != nil {
		t.Fatalf("compile %s%s: %v", file, ref, err)
	}
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, name string, raw json.RawMessage) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("%s does not validate: %v", name, err)
	}
}

// Every test operation maps to the same tool spec, MCP tool and vendor
// functions as its golden files, and its MCP tool is a valid Tool of the
// MCP schema.
func TestToolShapes(t *testing.T) {
	c := connectorstest.Contract()
	mcp := compile(t, "mcp-"+connectors.MCPRevision+".schema.json", "#/$defs/Tool")
	for _, op := range c.Operations {
		spec, _ := json.Marshal(connectors.ToolSpec(c, op))
		golden(t, op.ID+".toolspec.json", spec)
		tool := connectors.MCPTool(c, op)
		golden(t, op.ID+".mcp.json", tool)
		validate(t, mcp, op.ID+"'s MCP tool", tool)
		for _, vendor := range []string{connectors.VendorGemini, connectors.VendorAnthropic, connectors.VendorOpenAI} {
			f, err := connectors.FunctionSpec(c, op, vendor)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, op.ID+"."+vendor+".json", f)
		}
		if name := connectors.WireName(connectors.ToolName(c.ID, op.ID)); len(name) > connectors.MaxToolName || strings.Contains(name, ".") {
			t.Errorf("wire name %q", name)
		}
	}
	// The schema does refuse a tool whose input is not an object
	bad, _ := jsonschema.UnmarshalJSON(strings.NewReader(`{"name":"x","inputSchema":{"type":"string"}}`))
	if mcp.Validate(bad) == nil {
		t.Error("the MCP schema accepted a tool whose input is a string")
	}
	if _, err := connectors.FunctionSpec(c, c.Operations[0], "acme"); err == nil {
		t.Error("an unknown vendor was mapped")
	}
	// The annotations follow the effect, destructive flag and key
	var echo, fail, start struct{ Annotations map[string]bool }
	_ = json.Unmarshal(connectors.MCPTool(c, c.Operations[0]), &echo)
	_ = json.Unmarshal(connectors.MCPTool(c, c.Operations[1]), &fail)
	_ = json.Unmarshal(connectors.MCPTool(c, c.Operations[2]), &start)
	if !echo.Annotations["readOnlyHint"] || !fail.Annotations["destructiveHint"] || !start.Annotations["openWorldHint"] || !start.Annotations["idempotentHint"] {
		t.Errorf("annotations: echo %v, fail %v, start %v", echo.Annotations, fail.Annotations, start.Annotations)
	}
}

// The test contract as OpenAPI 3.1, its events as webhooks and start's
// callback as a callback, validates and matches its golden file.
func TestOpenAPI(t *testing.T) {
	doc := connectors.OpenAPI(connectorstest.Contract())
	golden(t, "test.echo.openapi.json", doc)
	validate(t, compile(t, "openapi-3.1.schema.json", ""), "the OpenAPI document", doc)
	var d struct {
		Webhooks map[string]any
		Paths    map[string]map[string]struct{ Callbacks map[string]any }
	}
	_ = json.Unmarshal(doc, &d)
	if d.Webhooks["finished"] == nil || d.Paths["/start"]["post"].Callbacks["finished"] == nil {
		t.Errorf("webhooks %v, start's callbacks %v", d.Webhooks, d.Paths["/start"])
	}
}
