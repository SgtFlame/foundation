package impl

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"unicode/utf8"
)

// minSection is the size below which a section is merged into the next one,
// so a heading directly followed by another does not make a chunk of its own.
const minSection = 256

// anchorRunes is the length of a chunk's Head and Tail anchors.
const anchorRunes = 48

// chunk is one piece of a file as the chunker cuts it.
type chunk struct {
	Start, End         int
	StartLine, EndLine int
	Heading            string
	Text               string
	Hash               string
	Head, Tail         string
}

// section is a heading and the text under it, up to the next heading.
type section struct {
	start, end int
	heading    string
}

// chunker splits markdown into chunks of at most size bytes, overlapping by
// overlap bytes. Chunks never cross a heading, so an edit only changes the
// chunks of its own section; every later chunk, and its embedding, stays the
// same.
type chunker struct {
	size, overlap int
}

func (c chunker) split(text string) []chunk {
	lines := lineStarts(text)
	var chunks []chunk
	for _, sec := range mergeSmall(sections(text)) {
		for _, r := range c.window(text, sec.start, sec.end) {
			body := text[r[0]:r[1]]
			if strings.TrimSpace(body) == "" {
				continue
			}
			chunks = append(chunks, chunk{
				Start:     r[0],
				End:       r[1],
				StartLine: lineOf(lines, r[0]),
				EndLine:   lineOf(lines, r[1]-1),
				Heading:   sec.heading,
				Text:      body,
				Hash:      hashText(body),
				Head:      head(body),
				Tail:      tail(body),
			})
		}
	}
	return chunks
}

// sections splits text at ATX headings outside fenced code. A section's
// heading is the path of headings above it, e.g. "Guide > Install".
func sections(text string) []section {
	var secs []section
	var stack []string // heading text by level - 1
	cur := section{}
	var fence string
	for pos := 0; pos < len(text); {
		end := strings.IndexByte(text[pos:], '\n')
		next := len(text)
		if end >= 0 {
			next = pos + end + 1
		}
		line := text[pos:next]
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
			fence = trimmed[:3]
		default:
			if level, title, ok := atxHeading(trimmed); ok && len(line)-len(trimmed) <= 3 {
				if pos > cur.start {
					cur.end = pos
					secs = append(secs, cur)
				}
				if level > len(stack) {
					stack = append(stack, make([]string, level-len(stack))...)
				}
				stack = append(stack[:level-1], title)
				cur = section{start: pos, heading: joinHeadings(stack)}
			}
		}
		pos = next
	}
	if len(text) > cur.start {
		cur.end = len(text)
		secs = append(secs, cur)
	}
	return secs
}

// atxHeading parses "## Title" into its level and title.
func atxHeading(line string) (level int, title string, ok bool) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := line[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' && rest[0] != '\n' && rest[0] != '\r' {
		return 0, "", false
	}
	title = strings.TrimSpace(rest)
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	return level, title, true
}

func joinHeadings(stack []string) string {
	var parts []string
	for _, h := range stack {
		if h != "" {
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, " > ")
}

// mergeSmall folds each section shorter than minSection into the next one,
// which keeps the later (deeper) heading.
func mergeSmall(secs []section) []section {
	var out []section
	for i := 0; i < len(secs); i++ {
		sec := secs[i]
		for sec.end-sec.start < minSection && i+1 < len(secs) {
			i++
			sec = section{start: sec.start, end: secs[i].end, heading: secs[i].heading}
		}
		out = append(out, sec)
	}
	return out
}

// window cuts [start, end) into ranges of at most c.size bytes, each starting
// c.overlap bytes before the previous one ended. Cuts fall after a blank line,
// a newline or a space when there is one in the second half of the window,
// and always on a UTF-8 boundary.
func (c chunker) window(text string, start, end int) [][2]int {
	var out [][2]int
	for pos := start; ; {
		if end-pos <= c.size {
			return append(out, [2]int{pos, end})
		}
		cut := bestCut(text, pos+c.size/2, pos+c.size)
		out = append(out, [2]int{pos, cut})

		next := cut - c.overlap
		if next <= pos {
			next = cut
		} else {
			next = wordStart(text, next, cut)
		}
		pos = next
	}
}

// bestCut is the end of a chunk in (min, max]: after the last blank line,
// newline or space, else the last rune boundary.
func bestCut(text string, min, max int) int {
	window := text[min:max]
	if i := strings.LastIndex(window, "\n\n"); i >= 0 {
		return min + i + 2
	}
	if i := strings.LastIndexByte(window, '\n'); i >= 0 {
		return min + i + 1
	}
	if i := strings.LastIndexByte(window, ' '); i >= 0 {
		return min + i + 1
	}
	for max > min && !utf8.RuneStart(text[max]) {
		max--
	}
	return max
}

// wordStart moves pos forward to the start of the next line or word before
// limit, so an overlap does not begin mid-word.
func wordStart(text string, pos, limit int) int {
	if i := strings.IndexByte(text[pos:limit], '\n'); i >= 0 && pos+i+1 < limit {
		return pos + i + 1
	}
	if i := strings.IndexByte(text[pos:limit], ' '); i >= 0 && pos+i+1 < limit {
		return pos + i + 1
	}
	for pos < limit && !utf8.RuneStart(text[pos]) {
		pos++
	}
	return pos
}

// lineStarts returns the byte offset of each line's start.
func lineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineOf is the 1-based line holding byte offset pos.
func lineOf(starts []int, pos int) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i] > pos })
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

// head is the first anchorRunes runes of the trimmed text, an exact substring
// of it to search for.
func head(text string) string {
	text = strings.TrimSpace(text)
	n := 0
	for i := range text {
		if n == anchorRunes {
			return text[:i]
		}
		n++
	}
	return text
}

// tail is the last anchorRunes runes of the trimmed text.
func tail(text string) string {
	text = strings.TrimSpace(text)
	end := len(text)
	for n := 0; n < anchorRunes && end > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(text[:end])
		end -= size
	}
	return text[end:]
}
