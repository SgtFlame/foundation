package kbtools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// kb.read reads a document as its derived Markdown, so a citation's lines
// are lines of that Markdown; kb.edit refuses a document (forge 35a-1).
func TestReadDocument(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "v"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "v", volumeservice.MarkerFile), []byte(`{"id":"1"}`), 0o644)
	_ = os.WriteFile(filepath.Join(root, "v", "sop.doc.json"), []byte(`{"format":1,"doc":{"type":"doc","content":[`+
		`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Refunds"}]},`+
		`{"type":"paragraph","content":[{"type":"text","text":"Ask a lead."}]}]}}`), 0o644)
	reg := extensions.NewRegistry()
	reg.Declare(tools.Tools)
	Contribute(reg, func() Deps { return Deps{Root: root} })
	byID := map[string]tools.Tool{}
	for _, tool := range tools.Tools.All(reg) {
		byID[tool.ID] = tool
	}
	call := func(name, args string) (tools.Result, error) {
		return byID[name].Run(tools.Call{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}, Name: name, Args: json.RawMessage(args)})
	}
	res, err := call("kb.read", `{"path":"v/sop.doc.json","startLine":3}`)
	if err != nil {
		t.Fatal(err)
	}
	seg := res.Segments[0]
	if seg.Text != "Ask a lead.\n" || seg.Location.Path != "v/sop.doc.json" || seg.Location.StartLine != 3 || res.Content["totalLines"] != 3 {
		t.Errorf("read %+v %+v", seg, res.Content)
	}
	if !strings.HasPrefix(seg.Title, "v/sop ›") {
		t.Errorf("title %q", seg.Title)
	}
	if _, err := call("kb.edit", `{"path":"v/sop.doc.json","edits":[{"find":"a","replace":"b"}]}`); err == nil || !strings.Contains(err.Error(), "only markdown") {
		t.Errorf("kb.edit on a document: %v", err)
	}
}
