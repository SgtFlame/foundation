// Package text holds algorithms over plain and markdown text that graphs
// use through its nodes (text/nodes, forge 18b): a Myers diff by lines or
// words whose hunks carry the markdown heading path they fall under, the
// sections of a markdown text, and Resolve, which finds a short quote again
// in a text that may have changed since it was taken.
//
// Offsets are in characters (Unicode code points) of the text, end
// exclusive; line numbers are 1-based.
package text

import (
	"strings"
	"unicode"
)

// Granularities a diff is made at.
const (
	Line = "line"
	Word = "word"
)

// Hunk operations.
const (
	OpInsert  = "insert"
	OpDelete  = "delete"
	OpReplace = "replace"
	// OpMove is a block deleted in one place and inserted unchanged in
	// another (line diffs only): A is where it was, B where it is.
	OpMove = "move"
)

// Range is a span of a text in characters, end exclusive.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Lines is a span of lines, 1-based and inclusive; empty (0, 0) when the
// hunk takes no line on that side.
type Lines struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Hunk is one change between two texts.
type Hunk struct {
	Op string `json:"op"`
	// A is the span in the old text (empty at the place for an insert), B in
	// the new (empty at the place for a delete).
	A      Range `json:"a"`
	B      Range `json:"b"`
	ALines Lines `json:"aLines"`
	BLines Lines `json:"bLines"`
	// Before is the old text of the span, After the new.
	Before string `json:"before"`
	After  string `json:"after"`
	// Text is what the change is about: After, or Before for a delete.
	Text string `json:"text"`
	// HeadingPath is the markdown headings the change falls under, outermost
	// first: in the new text, or the old for a delete.
	HeadingPath []string `json:"headingPath"`
}

// token is a piece of a text: a line, a word, a run of spaces or one mark.
type token struct {
	text  string
	key   string // what is compared
	start int    // character offset
	end   int
}

// Diff gives the hunks that turn a into b, at a granularity (Line, the
// default, or Word).
func Diff(a, b, granularity string) []Hunk {
	split := lines
	if granularity == Word {
		split = words
	}
	ta, tb := split(a), split(b)
	script := myers(ta, tb)
	ra, rb := []rune(a), []rune(b)
	hunks := group(script, ta, tb, granularity == Word)
	if granularity != Word {
		hunks = moves(hunks, ra, rb)
	}
	ha, hb := Headings(a), Headings(b)
	la, lb := lineStarts(a), lineStarts(b)
	for i := range hunks {
		h := &hunks[i]
		h.Before, h.After = string(ra[h.A.Start:h.A.End]), string(rb[h.B.Start:h.B.End])
		h.ALines, h.BLines = linesOf(la, h.A, len(ra)), linesOf(lb, h.B, len(rb))
		// The path at the hunk's first character that is not a space, so a
		// new section that the diff starts on the blank line before is under
		// its own heading.
		switch h.Op {
		case OpDelete:
			h.Text, h.HeadingPath = h.Before, pathAt(ha, firstInk(ra, h.A))
		default:
			h.Text, h.HeadingPath = h.After, pathAt(hb, firstInk(rb, h.B))
		}
	}
	return hunks
}

// lines splits a text into lines, each keeping its newline; lines compare
// without it, so a last line with and without one are the same.
func lines(s string) []token {
	var out []token
	pos := 0
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		line := s
		if i >= 0 {
			line = s[:i+1]
		}
		n := len([]rune(line))
		out = append(out, token{text: line, key: strings.TrimRight(line, "\r\n"), start: pos, end: pos + n})
		pos += n
		s = s[len(line):]
	}
	return out
}

// words splits a text into runs of letters and digits, runs of spaces
// (newlines among them), and single other characters.
func words(s string) []token {
	var out []token
	rs := []rune(s)
	class := func(r rune) int {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '’':
			return 1
		case unicode.IsSpace(r):
			return 2
		}
		return 3
	}
	for i := 0; i < len(rs); {
		c := class(rs[i])
		j := i + 1
		if c != 3 {
			for j < len(rs) && class(rs[j]) == c {
				j++
			}
		}
		t := string(rs[i:j])
		out = append(out, token{text: t, key: t, start: i, end: j})
		i = j
	}
	return out
}

// edit is one step of an edit script: '=' keeps a[i] (= b[j]), '-' deletes
// a[i], '+' inserts b[j].
type edit struct {
	op   byte
	i, j int
}

// maxD bounds the edit distance Myers searches; beyond it the differing
// middle is one replacement, which is still a correct diff.
const maxD = 4000

// myers is Myers' O(ND) diff over the tokens' keys, after taking off the
// common prefix and suffix.
func myers(a, b []token) []edit {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre].key == b[pre].key {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf].key == b[len(b)-1-suf].key {
		suf++
	}
	var out []edit
	for k := 0; k < pre; k++ {
		out = append(out, edit{'=', k, k})
	}
	out = append(out, middle(a[pre:len(a)-suf], b[pre:len(b)-suf], pre)...)
	for k := 0; k < suf; k++ {
		out = append(out, edit{'=', len(a) - suf + k, len(b) - suf + k})
	}
	return out
}

func middle(a, b []token, off int) []edit {
	n, m := len(a), len(b)
	whole := func() []edit {
		var out []edit
		for i := 0; i < n; i++ {
			out = append(out, edit{'-', off + i, 0})
		}
		for j := 0; j < m; j++ {
			out = append(out, edit{'+', 0, off + j})
		}
		return out
	}
	if n == 0 || m == 0 {
		return whole()
	}
	max := n + m
	if max > maxD {
		max = maxD
	}
	v := make([]int, 2*max+2)
	var trace [][]int
	found := false
	for d := 0; d <= max && !found; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[max+k-1] < v[max+k+1]) {
				x = v[max+k+1]
			} else {
				x = v[max+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x].key == b[y].key {
				x, y = x+1, y+1
			}
			v[max+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	if !found {
		return whole()
	}
	// Walk back through the trace.
	var rev []edit
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		vd := trace[d]
		k := x - y
		var pk int
		if k == -d || (k != d && vd[max+k-1] < vd[max+k+1]) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := vd[max+pk]
		py := px - pk
		for x > px && y > py {
			x, y = x-1, y-1
			rev = append(rev, edit{'=', off + x, off + y})
		}
		if d > 0 {
			if x == px {
				y--
				rev = append(rev, edit{'+', 0, off + y})
			} else {
				x--
				rev = append(rev, edit{'-', off + x, 0})
			}
		}
		x, y = px, py
	}
	out := make([]edit, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

// group turns an edit script into hunks; in a word diff, changes kept apart
// only by spaces are one hunk.
func group(script []edit, a, b []token, joinSpaces bool) []Hunk {
	var out []Hunk
	posA, posB := 0, 0 // character offsets reached
	var cur *Hunk
	flush := func() {
		if cur == nil {
			return
		}
		switch {
		case cur.A.Start == cur.A.End:
			cur.Op = OpInsert
		case cur.B.Start == cur.B.End:
			cur.Op = OpDelete
		default:
			cur.Op = OpReplace
		}
		out = append(out, *cur)
		cur = nil
	}
	for i := 0; i < len(script); i++ {
		e := script[i]
		if e.op == '=' {
			if cur != nil && joinSpaces && strings.TrimSpace(a[e.i].text) == "" {
				// Spaces between two changes join them.
				j := i
				for j < len(script) && script[j].op == '=' && strings.TrimSpace(a[script[j].i].text) == "" {
					j++
				}
				if j < len(script) && script[j].op != '=' {
					for ; i < j; i++ {
						posA, posB = a[script[i].i].end, b[script[i].j].end
					}
					cur.A.End, cur.B.End = posA, posB
					i--
					continue
				}
			}
			flush()
			posA, posB = a[e.i].end, b[e.j].end
			continue
		}
		if cur == nil {
			cur = &Hunk{A: Range{posA, posA}, B: Range{posB, posB}}
		}
		if e.op == '-' {
			posA = a[e.i].end
			cur.A.End = posA
		} else {
			posB = b[e.j].end
			cur.B.End = posB
		}
	}
	flush()
	return out
}

// moves pairs a deleted block with an inserted block of the same text (not
// blank, spaces at the ends aside) into one move.
func moves(hunks []Hunk, ra, rb []rune) []Hunk {
	used := make([]bool, len(hunks))
	norm := func(rs []rune, r Range) string { return strings.TrimSpace(string(rs[r.Start:r.End])) }
	for i, h := range hunks {
		if used[i] || h.Op != OpDelete {
			continue
		}
		key := norm(ra, h.A)
		if key == "" {
			continue
		}
		for j, g := range hunks {
			if used[j] || g.Op != OpInsert || norm(rb, g.B) != key {
				continue
			}
			used[i], used[j] = true, true
			hunks[i] = Hunk{Op: OpMove, A: h.A, B: g.B}
			break
		}
	}
	var out []Hunk
	for i, h := range hunks {
		if used[i] && h.Op != OpMove {
			continue
		}
		out = append(out, h)
	}
	return out
}

// lineStarts are the character offsets each line starts at.
func lineStarts(s string) []int {
	out := []int{0}
	for i, r := range []rune(s) {
		if r == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// lineAt is the 1-based line holding offset.
func lineAt(starts []int, off int) int {
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}

func linesOf(starts []int, r Range, n int) Lines {
	if r.Start == r.End {
		return Lines{}
	}
	end := r.End - 1
	if end >= n {
		end = n - 1
	}
	return Lines{Start: lineAt(starts, r.Start), End: lineAt(starts, end)}
}

// firstInk is the offset of the first character of a span that is not a
// space; the span's start when all are.
func firstInk(rs []rune, r Range) int {
	for i := r.Start; i < r.End; i++ {
		if !unicode.IsSpace(rs[i]) {
			return i
		}
	}
	return r.Start
}
