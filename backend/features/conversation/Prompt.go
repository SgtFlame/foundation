package conversation

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// maxContext bounds the context added to a prompt, in bytes, until levels
// give stage 2 a token budget (ContextRetrieval.md).
const maxContext = 48 << 10

// Prompt is the system prompt followed by header and the context: pinned
// segments first, in their provider's order (such as the checked Chronicle
// entries, then those stage 1 chose), then the rest best scored first, as
// much as fits. A segment two providers found goes in once, pinned if
// either pinned it. Citable items are numbered from start+1 in the order
// they go in, with cite after the header, and returned with those numbers.
func Prompt(system string, items []ContextItem, header, cite string, number func(retrieval.Scored) int) (string, []retrieval.Scored) {
	if len(items) == 0 {
		return system, nil
	}
	pinned := func(item ContextItem) bool { return item.Segment != nil && item.Segment.Pinned }
	slices.SortStableFunc(items, func(a, b ContextItem) int {
		switch {
		case pinned(a) && pinned(b):
			return 0 // in the order their provider chose
		case pinned(a):
			return -1
		case pinned(b):
			return 1
		}
		return cmp.Compare(b.Score, a.Score)
	})
	var b strings.Builder
	var cited []retrieval.Scored
	seen := map[[2]string]bool{}
	used := 0
	for _, item := range items {
		if seg := item.Segment; seg != nil && seg.Key != "" {
			if seen[[2]string{seg.SourceID, seg.Key}] {
				continue
			}
			seen[[2]string{seg.SourceID, seg.Key}] = true
		}
		if used+len(item.Text) > maxContext {
			continue
		}
		used += len(item.Text)
		title := item.Title
		if item.Segment != nil {
			seg := *item.Segment
			seg.Number = number(seg)
			cited = append(cited, seg)
			title = fmt.Sprintf("[%d] %s", seg.Number, title)
		}
		b.WriteString("\n## " + title + "\n" + strings.TrimSpace(item.Text) + "\n")
	}
	head := strings.TrimSpace(system) + "\n\n" + strings.TrimSpace(header) + "\n"
	if len(cited) > 0 && strings.TrimSpace(cite) != "" {
		head += strings.TrimSpace(cite) + "\n"
	}
	return head + b.String(), cited
}
