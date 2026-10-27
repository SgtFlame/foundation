package impl

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// paragraphs makes n paragraphs of about 200 bytes, with multi-byte runes.
func paragraphs(tag string, n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%s paragraph %d héllo wörld — ", tag, i)
		b.WriteString(strings.Repeat("lorem ipsum dolor ", 9))
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestChunkSizeOverlapAndBoundaries(t *testing.T) {
	text := "# Guide\n\n" + paragraphs("a", 60)
	c := chunker{size: 4096, overlap: 512}
	chunks := c.split(text)
	if len(chunks) < 3 {
		t.Fatalf("%d chunks from %d bytes", len(chunks), len(text))
	}
	for i, ch := range chunks {
		if ch.End-ch.Start > 4096 {
			t.Errorf("chunk %d is %d bytes", i, ch.End-ch.Start)
		}
		if !utf8.ValidString(ch.Text) || text[ch.Start:ch.End] != ch.Text {
			t.Errorf("chunk %d is not a valid slice of the text", i)
		}
		if ch.Heading != "Guide" {
			t.Errorf("chunk %d heading %q", i, ch.Heading)
		}
		if !strings.HasPrefix(strings.TrimSpace(ch.Text), ch.Head) || !strings.HasSuffix(strings.TrimSpace(ch.Text), ch.Tail) {
			t.Errorf("chunk %d anchors %q / %q", i, ch.Head, ch.Tail)
		}
		if got := strings.Count(text[:ch.Start], "\n") + 1; got != ch.StartLine {
			t.Errorf("chunk %d start line %d, want %d", i, ch.StartLine, got)
		}
		if i > 0 {
			prev := chunks[i-1]
			overlap := prev.End - ch.Start
			if overlap <= 0 || overlap > 512 {
				t.Errorf("chunks %d/%d overlap by %d bytes", i-1, i, overlap)
			}
		}
	}
	if last := chunks[len(chunks)-1]; last.End != len(text) {
		t.Errorf("last chunk ends at %d of %d", last.End, len(text))
	}
}

func TestChunkHeadings(t *testing.T) {
	text := "intro " + strings.Repeat("x ", 200) + "\n" +
		"# Top\n" + // small: merges into the next section
		"## Install\n" + strings.Repeat("step ", 100) + "\n" +
		"```\n# not a heading\n```\n" +
		"## Use\n" + strings.Repeat("run ", 100) + "\n"
	chunks := chunker{size: 4096, overlap: 512}.split(text)
	var headings []string
	for _, ch := range chunks {
		headings = append(headings, ch.Heading)
	}
	want := []string{"", "Top > Install", "Top > Use"}
	if strings.Join(headings, "|") != strings.Join(want, "|") {
		t.Fatalf("headings %q, want %q", headings, want)
	}
}

// An edit in one section leaves every other section's chunks unchanged.
func TestChunkEditStaysInSection(t *testing.T) {
	doc := func(middle string) string {
		return "# One\n\n" + paragraphs("one", 30) + "# Two\n\n" + middle + "# Three\n\n" + paragraphs("three", 30)
	}
	c := chunker{size: 4096, overlap: 512}
	before := c.split(doc(paragraphs("two", 30)))
	after := c.split(doc("inserted text\n\n" + paragraphs("two", 30)))

	hashes := func(chunks []chunk, heading string) []string {
		var out []string
		for _, ch := range chunks {
			if ch.Heading == heading {
				out = append(out, ch.Hash)
			}
		}
		return out
	}
	for _, h := range []string{"One", "Three"} {
		if fmt.Sprint(hashes(before, h)) != fmt.Sprint(hashes(after, h)) {
			t.Errorf("section %s changed", h)
		}
	}
	if fmt.Sprint(hashes(before, "Two")) == fmt.Sprint(hashes(after, "Two")) {
		t.Error("edited section kept its hashes")
	}
}

func TestChunkLongWordAndEmpty(t *testing.T) {
	if chunks := (chunker{size: 4096, overlap: 512}).split("  \n\n "); len(chunks) != 0 {
		t.Fatalf("blank text gave %d chunks", len(chunks))
	}
	text := strings.Repeat("é", 5000) // 10000 bytes and no spaces at all
	chunks := chunker{size: 4096, overlap: 512}.split(text)
	if len(chunks) != 3 || chunks[2].End != len(text) {
		t.Fatalf("got %d chunks", len(chunks))
	}
	for i, ch := range chunks {
		if !utf8.ValidString(ch.Text) {
			t.Errorf("chunk %d splits a rune", i)
		}
	}
}
