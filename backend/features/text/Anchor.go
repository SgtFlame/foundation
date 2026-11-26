package text

import (
	"strings"
	"unicode"
)

// Anchor is where a quote was found in a text.
type Anchor struct {
	Found bool `json:"found"`
	// Start and End are character offsets, end exclusive.
	Start int `json:"start"`
	End   int `json:"end"`
	// HeadingPath is the headings the place falls under.
	HeadingPath []string `json:"headingPath"`
	// Exact tells the quote was found as it is; otherwise it was found
	// with spaces, case, quotes or a few words different.
	Exact bool `json:"exact"`
	// Score is how alike the place and the quote are, 1 when exact.
	Score float64 `json:"score"`
}

// MinScore is how alike (by words in order) a place must be to a quote to
// be found by the fuzzy match.
const MinScore = 0.75

// maxQuoteWords bounds the quotes the fuzzy match tries.
const maxQuoteWords = 200

// Resolve finds a quote in a text: exactly under its heading path, then
// exactly anywhere, then with spaces, case and typographic quotes
// normalized, then by its words with a few changed (MinScore), preferring a
// place under the heading. Where it occurs more than once, context (words
// near it when it was taken) picks the occurrence.
func Resolve(s, quote string, headingPath []string, context string) Anchor {
	q := strings.TrimSpace(quote)
	if q == "" {
		return Anchor{HeadingPath: []string{}}
	}
	rs := []rune(s)
	hs := Headings(s)
	found := func(start, end int, exact bool, score float64) Anchor {
		return Anchor{Found: true, Start: start, End: end, Exact: exact, Score: score, HeadingPath: pathAt(hs, start)}
	}
	var within *Range
	if len(headingPath) > 0 {
		if sec, ok := SectionOf(s, headingPath); ok {
			within = &sec.Range
		}
	}
	qr := []rune(q)
	occ := occurrences(rs, qr)
	if within != nil {
		var in []int
		for _, o := range occ {
			if o >= within.Start && o+len(qr) <= within.End {
				in = append(in, o)
			}
		}
		if len(in) > 0 {
			o := best(rs, in, len(qr), context)
			return found(o, o+len(qr), true, 1)
		}
	}
	if len(occ) > 0 {
		o := best(rs, occ, len(qr), context)
		return found(o, o+len(qr), true, 1)
	}
	// Normalized.
	ns, idx := normalize(rs)
	nq, _ := normalize(qr)
	if len(nq) > 0 {
		nocc := occurrences(ns, nq)
		if len(nocc) > 0 {
			pick := nocc[0]
			if within != nil {
				for _, o := range nocc {
					if idx[o] >= within.Start && idx[o] < within.End {
						pick = o
						break
					}
				}
			}
			start, end := idx[pick], idx[pick+len(nq)-1]+1
			return found(start, end, false, 1)
		}
	}
	// Fuzzy, by words.
	tw := wordSpans(rs)
	qw := wordSpans(qr)
	if len(qw) == 0 || len(qw) > maxQuoteWords || len(tw) == 0 {
		return Anchor{HeadingPath: []string{}}
	}
	qk := make([]string, len(qw))
	for i, w := range qw {
		qk[i] = w.key
	}
	n := len(qk)
	inQuote := map[string]bool{}
	for _, k := range qk {
		inQuote[k] = true
	}
	bestScore, bestStart, bestEnd := 0.0, -1, -1
	slack := n / 4
	for size := n - slack; size <= n+slack; size++ {
		if size <= 0 || size > len(tw) {
			continue
		}
		for i := 0; i+size <= len(tw); i++ {
			if !inQuote[tw[i].key] && !inQuote[tw[i+size-1].key] {
				continue // a match starts or ends on one of the quote's words
			}
			score := 2 * float64(lcs(qk, tw[i:i+size])) / float64(n+size)
			if within != nil && tw[i].start >= within.Start && tw[i].start < within.End {
				score += 0.01 // under its heading wins a tie
			}
			if score > bestScore {
				bestScore, bestStart, bestEnd = score, tw[i].start, tw[i+size-1].end
			}
		}
	}
	if bestScore >= MinScore {
		if bestScore > 1 {
			bestScore = 1
		}
		return found(bestStart, bestEnd, false, bestScore)
	}
	return Anchor{HeadingPath: []string{}}
}

// occurrences are the offsets q occurs at in s.
func occurrences(s, q []rune) []int {
	var out []int
	if len(q) == 0 {
		return out
	}
	for i := 0; i+len(q) <= len(s); i++ {
		if s[i] != q[0] {
			continue
		}
		ok := true
		for j := 1; j < len(q); j++ {
			if s[i+j] != q[j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// best is the occurrence whose surroundings share most words with the
// context, each word counting less the further it is from the occurrence;
// the first when there is no context.
func best(s []rune, occ []int, n int, context string) int {
	if len(occ) == 1 || strings.TrimSpace(context) == "" {
		return occ[0]
	}
	want := map[string]bool{}
	for _, w := range wordSpans([]rune(context)) {
		want[w.key] = true
	}
	reach := len([]rune(context)) + 40
	pick, top := occ[0], -1.0
	for _, o := range occ {
		from, to := o-reach, o+n+reach
		if from < 0 {
			from = 0
		}
		if to > len(s) {
			to = len(s)
		}
		score := 0.0
		for _, w := range wordSpans(s[from:to]) {
			if !want[w.key] {
				continue
			}
			at, d := from+w.start, 0
			switch {
			case at+len(w.key) <= o:
				d = o - (at + len(w.key))
			case at >= o+n:
				d = at - (o + n)
			}
			score += 1 / (1 + float64(d)/10)
		}
		if score > top {
			pick, top = o, score
		}
	}
	return pick
}

// normalize lowers case, makes typographic quotes and dashes plain, drops
// markdown emphasis marks and collapses runs of spaces to one; idx maps each
// normalized character to its offset in s.
func normalize(s []rune) ([]rune, []int) {
	var out []rune
	var idx []int
	space := true // drops leading spaces
	for i, r := range s {
		switch r {
		case '‘', '’', '‛':
			r = '\''
		case '“', '”', '„':
			r = '"'
		case '–', '—':
			r = '-'
		case '*', '_', '`':
			continue
		}
		if unicode.IsSpace(r) {
			if space {
				continue
			}
			space = true
			out, idx = append(out, ' '), append(idx, i)
			continue
		}
		space = false
		out, idx = append(out, unicode.ToLower(r)), append(idx, i)
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out, idx = out[:len(out)-1], idx[:len(idx)-1]
	}
	return out, idx
}

type wordSpan struct {
	key        string
	start, end int
}

// wordSpans are the words of s (letters and digits), lowered.
func wordSpans(s []rune) []wordSpan {
	var out []wordSpan
	for i := 0; i < len(s); {
		if !(unicode.IsLetter(s[i]) || unicode.IsDigit(s[i])) {
			i++
			continue
		}
		j := i
		for j < len(s) && (unicode.IsLetter(s[j]) || unicode.IsDigit(s[j])) {
			j++
		}
		out = append(out, wordSpan{key: strings.ToLower(string(s[i:j])), start: i, end: j})
		i = j
	}
	return out
}

// lcs is the length of the longest common subsequence of q and the words.
func lcs(q []string, w []wordSpan) int {
	prev := make([]int, len(w)+1)
	cur := make([]int, len(w)+1)
	for i := 1; i <= len(q); i++ {
		for j := 1; j <= len(w); j++ {
			switch {
			case q[i-1] == w[j-1].key:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(w)]
}
