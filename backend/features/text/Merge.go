package text

import "sort"

// Conflict is a place where both sides of a three-way merge changed the
// base's text differently (forge 18d).
type Conflict struct {
	Index int `json:"index"`
	// Base, Source and Target are the region's text in each.
	Base   string `json:"base"`
	Source string `json:"source"`
	Target string `json:"target"`
	// HeadingPath is the base's headings at the region.
	HeadingPath []string `json:"headingPath"`
	// BaseLines is the region's lines in the base (empty for a place
	// between lines).
	BaseLines Lines `json:"baseLines"`
	// Merged is where the region's text is in the merged text, and Text
	// what was put there: a resolution, else the preferred side.
	Merged Range  `json:"merged"`
	Text   string `json:"text"`
	// Resolved says a resolution was given for it.
	Resolved bool `json:"resolved"`
}

// MergeResult is a three-way merge.
type MergeResult struct {
	Merged    string     `json:"merged"`
	Conflicts []Conflict `json:"conflicts"`
	// Clean is true when every change applied without a conflict, or every
	// conflict was resolved.
	Clean bool `json:"clean"`
	// SourceChanged and TargetChanged say whether each side differs from
	// the base.
	SourceChanged bool `json:"sourceChanged"`
	TargetChanged bool `json:"targetChanged"`
}

// Merge3 merges the changes base→source and base→target by lines: a
// region only one side changed takes that side's text, a region both
// changed alike takes it once, and a region both changed differently is a
// conflict, filled with resolutions[index] when given, else with the
// target's text (prefer "source" for the source's). Changes on lines next
// to each other are not a conflict; an insertion at the edge of the other
// side's change is.
func Merge3(base, source, target string, resolutions map[int]string, prefer string) MergeResult {
	tb := lines(base)
	hs := group(myers(tb, lines(source)), tb, lines(source), false)
	ht := group(myers(tb, lines(target)), tb, lines(target), false)
	rb, rs, rt := []rune(base), []rune(source), []rune(target)

	type change struct {
		side int // 0 source, 1 target
		h    Hunk
	}
	var all []change
	for _, h := range hs {
		all = append(all, change{0, h})
	}
	for _, h := range ht {
		all = append(all, change{1, h})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].h.A.Start != all[j].h.A.Start {
			return all[i].h.A.Start < all[j].h.A.Start
		}
		return all[i].side < all[j].side
	})

	// overlaps says whether a change touches a region another side changed.
	overlaps := func(r Range, h Range) bool {
		if r.Start == r.End && h.Start == h.End {
			return r.Start == h.Start
		}
		if r.Start == r.End {
			return h.Start <= r.Start && r.Start <= h.End
		}
		if h.Start == h.End {
			return r.Start <= h.Start && h.Start <= r.End
		}
		return h.Start < r.End && r.Start < h.End
	}

	type cluster struct {
		r     Range
		sides [2][]Hunk
	}
	var clusters []cluster
	for _, c := range all {
		if n := len(clusters); n > 0 {
			cl := &clusters[n-1]
			other := cl.sides[1-c.side]
			joins := false
			for _, o := range other {
				if overlaps(c.h.A, o.A) {
					joins = true
					break
				}
			}
			// A change of the same side inside the cluster's span joins it
			// too, so a conflict covers both of its hunks.
			if !joins && len(cl.sides[c.side]) > 0 && len(other) > 0 && c.h.A.Start < cl.r.End {
				joins = true
			}
			if joins {
				cl.sides[c.side] = append(cl.sides[c.side], c.h)
				if c.h.A.End > cl.r.End {
					cl.r.End = c.h.A.End
				}
				continue
			}
		}
		cl := cluster{r: c.h.A}
		cl.sides[c.side] = []Hunk{c.h}
		clusters = append(clusters, cl)
	}

	// sideText is a side's text for the base region r, given its hunks in it.
	sideText := func(r Range, hunks []Hunk, rside []rune) string {
		out := []rune{}
		pos := r.Start
		for _, h := range hunks {
			out = append(out, rb[pos:h.A.Start]...)
			out = append(out, rside[h.B.Start:h.B.End]...)
			pos = h.A.End
		}
		out = append(out, rb[pos:r.End]...)
		return string(out)
	}

	res := MergeResult{Conflicts: []Conflict{}, Clean: true, SourceChanged: len(hs) > 0, TargetChanged: len(ht) > 0}
	heads := Headings(base)
	starts := lineStarts(base)
	merged := []rune{}
	pos := 0
	for _, cl := range clusters {
		merged = append(merged, rb[pos:cl.r.Start]...)
		pos = cl.r.End
		var s, t string
		switch {
		case len(cl.sides[1]) == 0:
			merged = append(merged, []rune(sideText(cl.r, cl.sides[0], rs))...)
			continue
		case len(cl.sides[0]) == 0:
			merged = append(merged, []rune(sideText(cl.r, cl.sides[1], rt))...)
			continue
		default:
			s, t = sideText(cl.r, cl.sides[0], rs), sideText(cl.r, cl.sides[1], rt)
		}
		if s == t {
			merged = append(merged, []rune(s)...)
			continue
		}
		c := Conflict{Index: len(res.Conflicts), Base: string(rb[cl.r.Start:cl.r.End]), Source: s, Target: t,
			HeadingPath: pathAt(heads, firstInk(rb, cl.r)), BaseLines: linesOf(starts, cl.r, len(rb))}
		text, ok := resolutions[c.Index]
		if ok {
			c.Resolved = true
		} else {
			res.Clean = false
			text = t
			if prefer == "source" {
				text = s
			}
		}
		c.Text = text
		c.Merged = Range{len(merged), len(merged) + len([]rune(text))}
		merged = append(merged, []rune(text)...)
		res.Conflicts = append(res.Conflicts, c)
	}
	merged = append(merged, rb[pos:]...)
	res.Merged = string(merged)
	return res
}
