package collabservice

import (
	"fmt"
	"strings"
)

// Edit replaces the one occurrence of Find with Replace.
type Edit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// ApplyEdits applies edits to text in order, each to the text the previous
// ones left. Each Find must occur exactly once, so an edit never lands in
// the wrong place; the browser's writer applies them the same way
// (frontend/axiom/src/editors/markdown/collab/edits.ts).
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
