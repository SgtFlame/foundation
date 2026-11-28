package prosemirror

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func corpus(t *testing.T) Node {
	t.Helper()
	data, err := os.ReadFile("testdata/sop.doc.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestMarkdownGolden(t *testing.T) {
	golden(t, "sop.md", Markdown(corpus(t)))
}

func TestPlainTextGolden(t *testing.T) {
	golden(t, "sop.txt", PlainText(corpus(t)))
}

func TestBlocksPointIntoMarkdown(t *testing.T) {
	d := Derive(corpus(t))
	lines := strings.Split(d.Markdown, "\n")
	if len(d.Blocks) == 0 || d.Blocks[0].ID != "b1" || d.Blocks[1].ID != "b2" {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	for _, b := range d.Blocks {
		if b.StartLine < 1 || b.EndLine < b.StartLine || b.EndLine > len(lines) {
			t.Fatalf("block %+v out of %d lines", b, len(lines))
		}
		if strings.TrimSpace(lines[b.StartLine-1]) == "" {
			t.Errorf("block %+v starts on a blank line", b)
		}
	}
	if b, ok := d.BlockAt(1); !ok || b.ID != "b1" {
		t.Errorf("line 1 is %+v %v", b, ok)
	}
	if _, ok := d.BlockAt(2); ok {
		t.Error("line 2 is the blank line between blocks")
	}
	// The table's lines all map to the table
	for _, b := range d.Blocks {
		if b.Type == "table" && b.EndLine-b.StartLine != 2 {
			t.Errorf("table spans %+v", b)
		}
	}
}

func TestParseAndEnvelope(t *testing.T) {
	bare := []byte(`{"type": "doc", "content": [{"type":"paragraph"}]}`)
	file, err := Envelope(bare)
	if err != nil || string(file) != `{"format":1,"doc":{"type":"doc","content":[{"type":"paragraph"}]}}` {
		t.Fatalf("envelope %s %v", file, err)
	}
	if again, _ := Envelope(file); string(again) != string(file) {
		t.Errorf("a file is not kept byte for byte: %s", again)
	}
	if _, err := Parse(Empty()); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"format":2,"doc":{"type":"doc"}}`, `{"type":"paragraph"}`, `{"format":1,"doc":{"type":"x"}}`, `nope`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

func TestMarksAndEscaping(t *testing.T) {
	doc := Node{Type: "doc", Content: []Node{{Type: "paragraph", Content: []Node{
		{Type: "text", Text: " both ", Marks: []Mark{{Type: "bold"}, {Type: "italic"}}},
		{Type: "text", Text: "a_b"},
		{Type: "text", Text: "x`y", Marks: []Mark{{Type: "code"}}},
	}}}}
	if got := Markdown(doc); got != " _**both**_ a\\_b``x`y``\n" {
		t.Errorf("got %q", got)
	}
	if got := PlainText(doc); got != "both a_bx`y" {
		t.Errorf("plain %q", got)
	}
}
