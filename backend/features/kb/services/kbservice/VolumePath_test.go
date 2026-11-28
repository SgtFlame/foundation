package kbservice

import "testing"

func TestSplitFile(t *testing.T) {
	for name, want := range map[string][2]string{
		"notes.md":                   {"notes", "markdown"},
		"notes.doc.json":             {"notes", "document"},
		"data.json":                  {"data", "json"},
		"cfg.yml":                    {"cfg", "yaml"},
		"a.b.md":                     {"", ""},
		".notes.md":                  {"", ""},
		"notes.txt":                  {"", ""},
		".notes.doc.json.index.json": {"", ""},
	} {
		base, typ, ok := SplitFile(name)
		if base != want[0] || typ != want[1] || ok != (want[1] != "") {
			t.Errorf("SplitFile(%q) = %q %q %v, want %v", name, base, typ, ok, want)
		}
	}
	p := NewVolumePath().Folder("v", "notes").Type("document")
	if got, err := p.Build(); err != nil || got != "v/notes.doc.json" {
		t.Errorf("Build = %q %v", got, err)
	}
}
