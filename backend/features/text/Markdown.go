package text

import (
	"strings"
	"unicode"
)

// Heading is one ATX markdown heading (# to ######) outside fenced code.
type Heading struct {
	Level int
	Title string
	// Start is the character offset of its line, End of the line's end.
	Start int
	End   int
	// Path is the headings it falls under and itself, outermost first.
	Path []string
}

// Headings are a markdown text's headings in order.
func Headings(s string) []Heading {
	var out []Heading
	var stack []Heading
	fence := ""
	for _, t := range lines(s) {
		line := strings.TrimRight(t.key, " \t")
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 {
			if f := fenceOf(trimmed); f != "" {
				switch {
				case fence == "":
					fence = f
				case strings.HasPrefix(trimmed, fence) && strings.TrimLeft(trimmed, fence[:1]) == "":
					fence = ""
				}
				continue
			}
		}
		if fence != "" || len(line)-len(trimmed) > 3 {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
			continue
		}
		title := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(trimmed[level:]), "#"))
		if title == "" {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, h := range stack {
			path = append(path, h.Title)
		}
		path = append(path, title)
		h := Heading{Level: level, Title: title, Start: t.start, End: t.end, Path: path}
		stack = append(stack, h)
		out = append(out, h)
	}
	return out
}

// fenceOf is the fence a line opens or closes (``` or ~~~, three or more),
// or "".
func fenceOf(line string) string {
	for _, c := range []string{"`", "~"} {
		n := 0
		for n < len(line) && line[n:n+1] == c {
			n++
		}
		if n >= 3 {
			return line[:n]
		}
	}
	return ""
}

// pathAt is the heading path at a character offset: the path of the last
// heading starting at or before it; empty before the first heading.
func pathAt(hs []Heading, off int) []string {
	path := []string{}
	for _, h := range hs {
		if h.Start > off {
			break
		}
		path = h.Path
	}
	return append([]string{}, path...)
}

// PathAt is the heading path of a markdown text at a character offset.
func PathAt(s string, off int) []string { return pathAt(Headings(s), off) }

// Section is a heading's part of a markdown text: from its heading line to
// the next heading of the same or a higher level (its sub-sections in it).
// The part before the first heading is the section with an empty path.
type Section struct {
	Path  []string `json:"headingPath"`
	Range Range    `json:"range"`
	Text  string   `json:"text"`
}

// SectionOf finds the section a heading path names: the heading with that
// whole path, else the first whose own title is the path's last, compared
// without case or spaces at the ends. An empty path is the whole text.
func SectionOf(s string, path []string) (Section, bool) {
	rs := []rune(s)
	if len(path) == 0 {
		return Section{Path: []string{}, Range: Range{0, len(rs)}, Text: s}, true
	}
	hs := Headings(s)
	pick := -1
	for i, h := range hs {
		if samePath(h.Path, path) {
			pick = i
			break
		}
	}
	if pick < 0 {
		last := path[len(path)-1]
		for i, h := range hs {
			if sameTitle(h.Title, last) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return Section{}, false
	}
	h := hs[pick]
	end := len(rs)
	for _, g := range hs[pick+1:] {
		if g.Level <= h.Level {
			end = g.Start
			break
		}
	}
	return Section{Path: append([]string{}, h.Path...), Range: Range{h.Start, end}, Text: string(rs[h.Start:end])}, true
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameTitle(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameTitle(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// ChangedSections are the parts of both texts that hunks touch, once each
// by heading path, in the order first met: each heading's own part, from
// its heading line to the next heading of any level (the text before the
// first heading has the empty path). For a model asked about a long text's
// changes, the parts that changed with their headings. A path only one text
// has gives "" on the other side.
func ChangedSections(a, b string, hunks []Hunk) []map[string]any {
	la, lb := leaves(a), leaves(b)
	seen := map[string]bool{}
	var paths [][]string
	touch := func(ls []leaf, r Range) {
		for _, l := range ls {
			if !(r.Start < l.r.End && r.End > l.r.Start) {
				continue
			}
			key := strings.ToLower(strings.Join(l.path, "\x00"))
			if !seen[key] {
				seen[key] = true
				paths = append(paths, l.path)
			}
		}
	}
	ra, rb := []rune(a), []rune(b)
	for _, h := range hunks {
		// The side a pure insert or delete has no text on is left out: the
		// other side names the section. Blank lines at a hunk's ends belong
		// to the sections around it.
		if h.Op != OpDelete {
			touch(lb, ink(rb, h.B))
		}
		if h.Op != OpInsert {
			touch(la, ink(ra, h.A))
		}
	}
	out := []map[string]any{}
	for _, p := range paths {
		out = append(out, map[string]any{"headingPath": p, "a": leafText(la, p), "b": leafText(lb, p)})
	}
	return out
}

// leaf is a heading's own part of a text.
type leaf struct {
	path []string
	r    Range
	text string
}

func leaves(s string) []leaf {
	rs := []rune(s)
	hs := Headings(s)
	var out []leaf
	first := len(rs)
	if len(hs) > 0 {
		first = hs[0].Start
	}
	if first > 0 || len(hs) == 0 {
		out = append(out, leaf{path: []string{}, r: Range{0, first}, text: string(rs[:first])})
	}
	for i, h := range hs {
		end := len(rs)
		if i+1 < len(hs) {
			end = hs[i+1].Start
		}
		out = append(out, leaf{path: h.Path, r: Range{h.Start, end}, text: string(rs[h.Start:end])})
	}
	return out
}

func leafText(ls []leaf, path []string) string {
	for _, l := range ls {
		if samePath(l.path, path) {
			return l.text
		}
	}
	return ""
}

// ink is a span without the spaces at its ends; the span itself when it is
// all spaces.
func ink(rs []rune, r Range) Range {
	start, end := r.Start, r.End
	for start < end && unicode.IsSpace(rs[start]) {
		start++
	}
	for end > start && unicode.IsSpace(rs[end-1]) {
		end--
	}
	if start == end {
		return r
	}
	return Range{start, end}
}

// Part is one heading's own part of a markdown text, for search (forge
// 18f): from its heading line to the next heading of any level, the text
// before the first heading being the part with an empty path. A part longer
// than a limit is cut at blank lines into pieces sharing its path.
type Part struct {
	Path      []string `json:"headingPath"`
	Heading   string   `json:"heading"` // its own title; "" before the first heading
	Text      string   `json:"text"`
	StartLine int      `json:"startLine"` // 1-based
	EndLine   int      `json:"endLine"`
}

// Parts are a markdown text's parts in order, each at most maxChars runes
// when it can be cut at a blank line (0: no limit), without the parts that
// hold nothing but spaces.
func Parts(s string, maxChars int) []Part {
	starts := lineStarts(s)
	rs := []rune(s)
	var out []Part
	for _, l := range leaves(s) {
		heading := ""
		if len(l.path) > 0 {
			heading = l.path[len(l.path)-1]
		}
		for _, r := range cut(rs, l.r, maxChars) {
			t := string(rs[r.Start:r.End])
			if strings.TrimSpace(t) == "" {
				continue
			}
			in := ink(rs, r)
			out = append(out, Part{Path: append([]string{}, l.path...), Heading: heading, Text: strings.TrimRight(t, "\n "),
				StartLine: lineAt(starts, in.Start), EndLine: lineAt(starts, max(in.Start, in.End-1))})
		}
	}
	return out
}

// cut splits r into pieces of at most max runes at blank lines; a piece
// with no blank line to cut at stays whole.
func cut(rs []rune, r Range, limit int) []Range {
	if limit <= 0 || r.End-r.Start <= limit {
		return []Range{r}
	}
	var out []Range
	start := r.Start
	for r.End-start > limit {
		at := -1
		for i := start + limit; i > start+1; i-- {
			if rs[i-1] == '\n' && rs[i-2] == '\n' {
				at = i
				break
			}
		}
		if at < 0 {
			break
		}
		out = append(out, Range{start, at})
		start = at
	}
	return append(out, Range{start, r.End})
}
