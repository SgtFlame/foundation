package records

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"
)

// TestRankBetweenKeepsAStrictOrder inserts 10 000 ranks at random places
// in one order: every new rank sorts strictly between its neighbours, as
// text and as sort tokens.
func TestRankBetweenKeepsAStrictOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(30, 1))
	order := []string{}
	longest := 0
	for i := range 10_000 {
		at := r.IntN(len(order) + 1)
		if i%5 == 0 {
			at = len(order) / 2 // hammer one gap too
		}
		after, before := "", ""
		if at > 0 {
			after = order[at-1]
		}
		if at < len(order) {
			before = order[at]
		}
		x, err := RankBetween(after, before)
		if err != nil {
			t.Fatalf("between %q and %q: %v", after, before, err)
		}
		if err := CheckRank(x); err != nil {
			t.Fatalf("%q: %v", x, err)
		}
		if (after != "" && x <= after) || (before != "" && x >= before) {
			t.Fatalf("%q is not between %q and %q", x, after, before)
		}
		order = slices.Insert(order, at, x)
		longest = max(longest, len(x))
	}
	if !sort.StringsAreSorted(order) {
		t.Fatal("the ranks are out of order")
	}
	f := Field{ID: "p", Name: "Position", Type: "rank"}
	for i := 1; i < len(order); i++ {
		a, _ := SortToken(f, mustJSON(order[i-1]))
		b, _ := SortToken(f, mustJSON(order[i]))
		if a >= b {
			t.Fatalf("tokens of %q and %q are out of order", order[i-1], order[i])
		}
	}
	t.Logf("longest rank %d", longest)
}

func TestRankChecksAndEdges(t *testing.T) {
	for _, bad := range []string{"", "a0", "a-b", string(make([]byte, MaxRank+1))} {
		if CheckRank(bad) == nil {
			t.Errorf("%q passed as a rank", bad)
		}
	}
	if _, err := RankBetween("b", "a"); err == nil {
		t.Error("a rank between b and a was made")
	}
	// Two neighbours one digit apart still have room between them
	x, err := RankBetween("a", "a1")
	if err != nil || x <= "a" || x >= "a1" {
		t.Errorf("between a and a1 = %q, %v", x, err)
	}
	// RankAmong forgives neighbours that moved
	if x, err := RankAmong("m", "c"); err != nil || x <= "c" || x >= "m" {
		t.Errorf("among m and c = %q, %v", x, err)
	}
	if x, err := RankAmong("m", "m"); err != nil || x <= "m" {
		t.Errorf("among m and m = %q, %v", x, err)
	}
	// Concurrent moves into one gap rarely tie
	seen := map[string]bool{}
	for range 200 {
		x, _ := RankBetween("a", "b")
		seen[x] = true
	}
	if len(seen) < 190 {
		t.Errorf("200 ranks between a and b gave only %d distinct", len(seen))
	}
}

func TestEvenRanks(t *testing.T) {
	for _, n := range []int{1, 2, 61, 62, 900, 5000} {
		rs := EvenRanks(n)
		if len(rs) != n || !sort.StringsAreSorted(rs) || len(slices.Compact(slices.Clone(rs))) != n {
			t.Fatalf("%d even ranks are not %d distinct ordered ranks", n, n)
		}
		for _, x := range rs {
			if err := CheckRank(x); err != nil || len(x) > 3 {
				t.Fatalf("%q: %v", x, err)
			}
		}
	}
}

func TestRankValuesAndConversion(t *testing.T) {
	f := Field{ID: "p", Name: "Position", Type: "rank", Scope: "status"}
	if _, err := CheckValue(f, mustJSON("V")); err != nil {
		t.Error(err)
	}
	if _, err := CheckValue(f, mustJSON("V0")); err == nil {
		t.Error("a rank ending in 0 was accepted")
	}
	// Numbers become ranks in their order
	num := Field{ID: "p", Name: "Position", Type: "number"}
	var prev string
	for _, n := range []float64{-1e9, -2.5, -1, 0, 0.5, 1, 2, 3.75, 1e12} {
		out, err := Convert(num, f, mustJSON(n))
		if err != nil {
			t.Fatalf("%v: %v", n, err)
		}
		var s string
		_ = json.Unmarshal(out, &s)
		if prev != "" && s <= prev {
			t.Fatalf("%v became %q, not after %q", n, s, prev)
		}
		prev = s
	}
	// A scope must be another sortable field
	e := EntityType{ID: "task", Name: "Task", Fields: []Field{{ID: "title", Name: "Title", Type: "text"}, {ID: "p", Name: "Position", Type: "rank", Scope: "nope"}}}
	if ps := Check(e, nil); len(ps) != 1 || ps[0].Path != "fields.p.scope" {
		t.Errorf("problems = %+v", ps)
	}
	e.Fields[1].Scope = "title"
	e.Indexes = []Index{{ID: "byPos", Fields: []string{"title", "p"}}}
	if ps := Check(e, nil); len(ps) != 0 {
		t.Errorf("problems = %+v", ps)
	}
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
