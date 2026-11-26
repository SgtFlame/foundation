package collabservice

import (
	"fmt"
	"strings"
)

// Edit replaces the one occurrence of Find with Replace. EditDoc also
// takes a range (forge 18c): Start and End, character offsets (runes, End
// exclusive) in the text as it is saved, replaced by Replace; Find, when
// given with a range, is the text expected there. A range is turned into a
// Find unique in the text (Unique) before a writer sees it.
type Edit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Start   *int   `json:"start,omitempty"`
	End     *int   `json:"end,omitempty"`
}

// ApplyEdits applies edits to text in order, each to the text the previous
// ones left. Each Find must occur exactly once, so an edit never lands in
// the wrong place; the browser's writer applies them the same way
// (frontend/common/src/editors/markdown/collab/edits.ts).
func ApplyEdits(text string, edits []Edit) (string, error) {
	for i, e := range edits {
		if e.Find == "" {
			return "", fmt.Errorf("edit %d has nothing to find", i+1)
		}
		switch n := strings.Count(text, e.Find); n {
		case 1:
			text = strings.Replace(text, e.Find, e.Replace, 1)
		case 0:
			return "", fmt.Errorf("edit %d: the text to find is not in the file", i+1)
		default:
			return "", fmt.Errorf("edit %d: the text to find occurs %d times; include more of the text around it", i+1, n)
		}
	}
	return text, nil
}

// Prepared is edits decided against a text (forge 18c): the find/replace
// edits that apply, in order, each to the text the ones before left;
// whether each edit given applied, and why not; and the text they make.
type Prepared struct {
	Edits    []Edit
	Applied  []bool
	Problems []string
	Text     string
}

// Prepare decides edits against text, in order. A range edit becomes a
// find/replace edit whose Find is the range grown by the fewest characters
// around it that make it occur once (Unique). Each edit that cannot apply
// (a find missing or occurring more than once, a range outside the text or
// not holding the text expected) is skipped with its reason, and the rest
// still apply.
func Prepare(text string, edits []Edit) Prepared {
	out := Prepared{Applied: make([]bool, len(edits)), Problems: make([]string, len(edits))}
	for i, e := range edits {
		find, replace := e.Find, e.Replace
		if e.Start != nil || e.End != nil {
			rs := []rune(text)
			start, end := 0, 0
			if e.Start != nil {
				start = *e.Start
			}
			end = start
			if e.End != nil {
				end = *e.End
			}
			if start < 0 || end < start || end > len(rs) {
				out.Problems[i] = fmt.Sprintf("edit %d: the place %d to %d is outside the text", i+1, start, end)
				continue
			}
			if e.Find != "" && string(rs[start:end]) != e.Find {
				out.Problems[i] = fmt.Sprintf("edit %d: the text there has changed", i+1)
				continue
			}
			var ok bool
			if find, replace, ok = Unique(text, start, end, e.Replace); !ok {
				out.Problems[i] = fmt.Sprintf("edit %d: an empty text has nothing to find", i+1)
				continue
			}
		}
		next, err := ApplyEdits(text, []Edit{{Find: find, Replace: replace}})
		if err != nil {
			out.Problems[i] = strings.Replace(err.Error(), "edit 1", fmt.Sprintf("edit %d", i+1), 1)
			continue
		}
		text = next
		out.Applied[i] = true
		out.Edits = append(out.Edits, Edit{Find: find, Replace: replace})
	}
	out.Text = text
	return out
}

// Unique turns replacing the runes start to end of text with replace into
// a find/replace edit whose Find occurs once in text: the range grown a
// character at a time, to the right then the left, until it is unique.
// It is false only for an empty text.
func Unique(text string, start, end int, replace string) (find, with string, ok bool) {
	rs := []rune(text)
	if len(rs) == 0 {
		return "", "", false
	}
	a, b := start, end
	unique := func() bool { return a < b && strings.Count(text, string(rs[a:b])) == 1 }
	// The whole text occurs once in itself, so this ends
	for !unique() {
		if b < len(rs) {
			b++
			if unique() {
				break
			}
		}
		if a > 0 {
			a--
		}
	}
	return string(rs[a:b]), string(rs[a:start]) + replace + string(rs[end:b]), true
}
