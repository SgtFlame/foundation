package git

import "testing"

func TestCleanPath(t *testing.T) {
	for _, p := range []string{"a.md", "www/content/code/x/index.md", ".github/workflows/deploy.yml"} {
		if _, err := CleanPath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"", "/etc/passwd", "a/../b", "./a", "a//b", "a/", ".git/config", "x/.GIT/y", `a\b`} {
		if _, err := CleanPath(p); err == nil {
			t.Errorf("%q is accepted", p)
		}
	}
}

func TestBranchName(t *testing.T) {
	for in, want := range map[string]string{"main": "main", "refs/heads/docs/post": "docs/post", "suite/one-1": "suite/one-1"} {
		if got, err := BranchName(in); err != nil || got != want {
			t.Errorf("%q gave %q, %v", in, got, err)
		}
	}
	for _, b := range []string{"", "HEAD", "a..b", "a b", "a~1", "x.lock", "/a", "a/", "a/.b", "a@{1}", "a:b"} {
		if _, err := BranchName(b); err == nil {
			t.Errorf("%q is accepted", b)
		}
	}
}

func TestKey(t *testing.T) {
	m := WithKey("Publish a post\n", "run-1")
	if !HasKey(m, "run-1") || HasKey(m, "run-2") || HasKey("Publish", "") || WithKey("x", "") != "x" {
		t.Fatalf("trailer %q", m)
	}
}

func TestList(t *testing.T) {
	files := map[string]Entry{}
	for _, p := range []string{"README.md", "a/b.md", "a/c/d.md", "a/c/e.md", "z.md"} {
		files[p] = Entry{Path: p, Kind: "file"}
	}
	page, next, err := List(files, "", false, "", 0)
	if err != nil || len(page) != 3 || page[0].Path != "README.md" || page[1].Kind != "dir" || next != "" {
		t.Fatalf("root: %+v %q %v", page, next, err)
	}
	page, next, _ = List(files, "a", true, "", 2)
	if len(page) != 2 || next != "a/c/d.md" {
		t.Fatalf("first page: %+v %q", page, next)
	}
	page, next, _ = List(files, "a", true, next, 2)
	if len(page) != 1 || page[0].Path != "a/c/e.md" || next != "" {
		t.Fatalf("second page: %+v %q", page, next)
	}
	if _, _, err := List(files, "nope", false, "", 0); err == nil {
		t.Fatal("a missing folder lists")
	}
	if _, _, err := List(files, "z.md", false, "", 0); err == nil {
		t.Fatal("a file lists as a folder")
	}
}

func TestDiff(t *testing.T) {
	got := Diff(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1", "b": "3", "c": "4"})
	if len(got) != 2 || got[0] != (Change{"b", Modified}) || got[1] != (Change{"c", Added}) {
		t.Fatalf("%+v", got)
	}
}
