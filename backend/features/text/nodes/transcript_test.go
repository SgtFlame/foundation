package textnodes_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/text"
	textnodes "github.com/SgtFlame/foundation/backend/features/text/nodes"
)

// fakeReader parses in this process, as a test may, and records its calls.
type fakeReader struct {
	calls    int
	name, ct string
	raw      string
}

func (f *fakeReader) read(_ context.Context, name, ct string, raw []byte) (text.Transcript, error) {
	f.calls++
	f.name, f.ct, f.raw = name, ct, string(raw)
	return text.ParseTranscript(name, ct, raw)
}

func transcriptNode(t *testing.T, deps func() textnodes.TranscriptDeps) cognition.NodeType {
	t.Helper()
	reg := extensions.NewRegistry()
	textnodes.ContributeTranscripts(reg, deps)
	for _, n := range cognition.Nodes.All(reg) {
		if n.ID == textnodes.TranscriptID {
			if p := cognition.MetaProblems(n); len(p) > 0 {
				t.Fatalf("metadata: %v", p)
			}
			return n
		}
	}
	t.Fatal("no Parse transcript")
	return cognition.NodeType{}
}

func exec() *cognition.Exec {
	p := features.LocalPrincipal()
	return &cognition.Exec{ServiceArgs: features.ServiceArgs{Ctx: context.Background(), Principal: p}}
}

func TestTranscriptNodeText(t *testing.T) {
	r := &fakeReader{}
	nt := transcriptNode(t, func() textnodes.TranscriptDeps { return textnodes.TranscriptDeps{Read: r.read} })
	out, pin, err := nt.Run(exec(), cognition.Values{"text": "Anna: Hello.\nBen: Hi.", "name": "pasted.txt"})
	if err != nil || pin != cognition.Then || out["ok"] != true || r.calls != 1 || r.name != "pasted.txt" {
		t.Fatalf("%v %q %v calls %d", out, pin, err, r.calls)
	}
	lines := out["lines"].([]any)
	if len(lines) != 2 || lines[1].(map[string]any)["speaker"] != "Ben" || lines[1].(map[string]any)["n"] != float64(2) {
		t.Errorf("lines %v", lines)
	}
	if sp := out["speakers"].([]any); len(sp) != 2 || sp[0] != "Anna" || out["format"] != "text" {
		t.Errorf("out %v", out)
	}
	// A parse error is reported, not a node error
	out, pin, err = nt.Run(exec(), cognition.Values{"text": "{\"x\":", "name": "a.json"})
	if err != nil || pin != cognition.Then || out["ok"] != false || out["error"] != text.ErrTranscriptJSON.Error() {
		t.Errorf("parse error: %v %v", out, err)
	}
}

func TestTranscriptNodeFile(t *testing.T) {
	r := &fakeReader{}
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n<v Anna>Hi.</v>\n"
	var tenant uint64
	open := func(_ context.Context, tn uint64, id string) (io.ReadCloser, records.FileRef, bool, error) {
		tenant = tn
		switch id {
		case "f1":
			return io.NopCloser(strings.NewReader(vtt)), records.FileRef{ID: "f1", Name: "call.vtt", ContentType: "text/vtt"}, true, nil
		case "big":
			return io.NopCloser(strings.NewReader(strings.Repeat("x", text.MaxTranscriptBytes+10))), records.FileRef{ID: "big", Name: "big.txt"}, true, nil
		case "broken":
			return nil, records.FileRef{}, false, errors.New("bucket down")
		}
		return nil, records.FileRef{}, false, nil
	}
	nt := transcriptNode(t, func() textnodes.TranscriptDeps { return textnodes.TranscriptDeps{Read: r.read, Open: open} })
	out, _, err := nt.Run(exec(), cognition.Values{"file": map[string]any{"id": "f1", "name": "call.vtt", "size": 60, "contentType": "text/vtt"}, "text": "ignored"})
	if err != nil || out["ok"] != true || out["format"] != "vtt" || r.raw != vtt || r.ct != "text/vtt" || tenant != features.LocalPrincipal().TenantID {
		t.Fatalf("file: %v %v %+v", out, err, r)
	}
	for id, want := range map[string]string{"gone": "missing", "big": "larger than 10 MB", "broken": "could not be opened"} {
		calls := r.calls
		out, pin, err := nt.Run(exec(), cognition.Values{"file": id})
		if err != nil || pin != cognition.Then || out["ok"] != false || !strings.Contains(out["error"].(string), want) || r.calls != calls {
			t.Errorf("%s: %v %v", id, out, err)
		}
	}
	if out, _, _ := nt.Run(exec(), cognition.Values{}); out["ok"] != false || !strings.Contains(out["error"].(string), "no file or text") {
		t.Errorf("nothing given: %v", out)
	}
}

func TestTranscriptNodeNotConfigured(t *testing.T) {
	nt := transcriptNode(t, func() textnodes.TranscriptDeps { return textnodes.TranscriptDeps{} })
	if out, _, err := nt.Run(exec(), cognition.Values{"text": "Anna: hi"}); err != nil || out["ok"] != false || !strings.Contains(out["error"].(string), "cannot be read here") {
		t.Errorf("no reader: %v %v", out, err)
	}
	if out, _, _ := nt.Run(exec(), cognition.Values{"file": "f1"}); out["ok"] != false || !strings.Contains(out["error"].(string), "files cannot be read") {
		t.Errorf("no files: %v", out)
	}
	// A nil deps function is no reader either
	nt = transcriptNode(t, nil)
	if out, _, _ := nt.Run(exec(), cognition.Values{"text": "Anna: hi"}); out["ok"] != false {
		t.Errorf("nil deps: %v", out)
	}
}
