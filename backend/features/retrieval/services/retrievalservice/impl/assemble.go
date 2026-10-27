package impl

import (
	"cmp"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// assemble builds the window from the scored candidates: best first, it
// merges chunks of one file whose byte ranges overlap or touch (the index's
// chunks overlap, so the same text would go in twice), keeps identical text
// from different files once, citing every path, then takes segments until
// topK or the token budget runs out, skipping one that does not fit, and
// numbers them for citation.
func assemble(candidates []retrieval.Scored, topK, budget int) []retrieval.Scored {
	slices.SortStableFunc(candidates, func(a, b retrieval.Scored) int { return cmp.Compare(b.Score, a.Score) })
	var kept []*retrieval.Scored
	byKey := map[[2]string]*retrieval.Scored{}
	for _, c := range candidates {
		if c.Key != "" {
			if same := byKey[[2]string{c.SourceID, c.Key}]; same != nil {
				if c.Location.Path != "" && c.Location.Path != same.Location.Path && !slices.Contains(same.Also, c.Location.Path) {
					same.Also = append(same.Also, c.Location.Path)
				}
				continue
			}
		}
		merged := false
		for _, k := range kept {
			if merge(k, c) {
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		seg := c
		kept = append(kept, &seg)
		if c.Key != "" {
			byKey[[2]string{c.SourceID, c.Key}] = &seg
		}
	}

	var out []retrieval.Scored
	used := 0
	for _, k := range kept {
		if len(out) == topK {
			break
		}
		if k.Tokens > budget-used {
			continue
		}
		used += k.Tokens
		k.Number = len(out) + 1
		out = append(out, *k)
	}
	return out
}

// merge folds c into k when both are byte ranges of the same file that
// overlap or touch, and both texts match their ranges (the file is
// unchanged since it was indexed). k keeps its score, the higher one.
func merge(k *retrieval.Scored, c retrieval.Scored) bool {
	a, b := k.Location, c.Location
	if k.SourceID != c.SourceID || a.Path == "" || a.Path != b.Path || a.End <= a.Start || b.End <= b.Start ||
		len(k.Text) != a.End-a.Start || len(c.Text) != b.End-b.Start || b.Start > a.End || b.End < a.Start {
		return false
	}
	text := k.Text
	if b.Start < a.Start {
		text = c.Text[:a.Start-b.Start] + text
		k.Location.Start, k.Location.StartLine = b.Start, b.StartLine
	}
	if b.End > a.End {
		text += c.Text[a.End-b.Start:]
		k.Location.End, k.Location.EndLine = b.End, b.EndLine
	}
	k.Text = text
	k.Tokens = tokens(text)
	if c.Key != "" && !slices.Contains(k.Keys, c.Key) {
		k.Keys = append(k.Keys, c.Key)
	}
	return true
}
