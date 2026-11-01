package records_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/records"
)

func intPtr(n int) *int { return &n }

// sortsAs checks that every pair of values' tokens compare as want says
// the values do.
func sortsAs(t *testing.T, f records.Field, values []any, want func(a, b any) int) {
	t.Helper()
	for _, a := range values {
		for _, b := range values {
			ra, _ := json.Marshal(a)
			rb, _ := json.Marshal(b)
			ta, err := records.SortToken(f, ra)
			if err != nil {
				t.Fatalf("%s %v: %v", f.Type, a, err)
			}
			tb, _ := records.SortToken(f, rb)
			if got, w := sign(strings.Compare(ta, tb)), sign(want(a, b)); got != w {
				t.Fatalf("%s: %v vs %v compares %d, want %d (%s vs %s)", f.Type, a, b, got, w, ta, tb)
			}
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestEncodingsSortAsTheirValues(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))

	numbers := []any{0.0, -0.0, 1.0, -1.0, 0.5, -0.5, 1e-300, -1e-300, 1e300, -1e300, 3.25, 3.5, -3.25, -3.5, 100.0, -100.0}
	for range 40 {
		numbers = append(numbers, (r.Float64()-0.5)*float64(r.IntN(1e6)))
	}
	sortsAs(t, records.Field{Type: "number"}, numbers, func(a, b any) int { return big.NewFloat(a.(float64)).Cmp(big.NewFloat(b.(float64))) })

	// Decimals of different scales, as stored (a string with the field's
	// scale): one field holds one scale, but the order must not depend on it
	decimals := []any{"0", "0.00", "-0.0001", "0.0001", "1", "1.0", "1.25", "1.2", "-1.2", "-1.25", "10", "9.99", "-10", "-9.99", "0.05", "0.5", "-0.05", "-0.5", "123456789012345678901234567890.5", "-123456789012345678901234567890.5"}
	for range 40 {
		scale := r.IntN(5)
		n := new(big.Rat).SetFrac64(r.Int64N(2_000_000)-1_000_000, int64(r.IntN(1000)+1))
		decimals = append(decimals, n.FloatString(scale))
	}
	byRat := func(a, b any) int {
		x, _ := new(big.Rat).SetString(a.(string))
		y, _ := new(big.Rat).SetString(b.(string))
		return x.Cmp(y)
	}
	for _, scale := range []int{0, 2, 4} {
		sortsAs(t, records.Field{Type: "decimal", Scale: intPtr(scale)}, decimals, byRat)
	}
	sortsAs(t, records.Field{Type: "money", Currency: "USD"}, decimals, byRat)

	var dates, times []any
	for range 60 {
		at := time.Date(1600+r.IntN(800), time.Month(r.IntN(12)+1), r.IntN(28)+1, r.IntN(24), r.IntN(60), r.IntN(60), r.IntN(1e9), time.UTC)
		dates = append(dates, at.Format(time.DateOnly))
		times = append(times, at.Format(time.RFC3339Nano))
	}
	times = append(times, "2026-11-01T09:30:00Z", "2026-11-01T09:30:00.1Z", "2026-11-01T09:30:00.12Z", "1969-12-31T23:59:59.999Z", "1970-01-01T00:00:00Z")
	sortsAs(t, records.Field{Type: "date"}, dates, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	sortsAs(t, records.Field{Type: "dateTime"}, times, func(a, b any) int {
		x, _ := time.Parse(time.RFC3339Nano, a.(string))
		y, _ := time.Parse(time.RFC3339Nano, b.(string))
		return x.Compare(y)
	})

	var durations []any
	for _, d := range []time.Duration{0, -time.Second, time.Second, 90 * time.Minute, -90 * time.Minute, time.Nanosecond} {
		durations = append(durations, d.String())
	}
	sortsAs(t, records.Field{Type: "duration"}, durations, func(a, b any) int {
		x, _ := time.ParseDuration(a.(string))
		y, _ := time.ParseDuration(b.(string))
		return sign(int(x - y))
	})

	// Text sorts ignoring case, then as written
	text := []any{"", "a", "A", "ab", "Ab", "b", "B", "apple", "Banana", "banana", "zebra", "é", "e", "a b", "a-b"}
	sortsAs(t, records.Field{Type: "text"}, text, func(a, b any) int {
		x, y := a.(string), b.(string)
		if c := strings.Compare(strings.ToLower(x), strings.ToLower(y)); c != 0 {
			return c
		}
		return strings.Compare(x, y)
	})

	level := records.Field{Type: "enum", Options: []records.Option{{ID: "low"}, {ID: "medium"}, {ID: "high"}}}
	sortsAs(t, level, []any{"low", "medium", "high"}, func(a, b any) int {
		rank := map[any]int{"low": 0, "medium": 1, "high": 2}
		return rank[a] - rank[b]
	})
	sortsAs(t, records.Field{Type: "bool"}, []any{false, true}, func(a, b any) int {
		return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) // false before true
	})

	// No value sorts before every value
	none, _ := records.SortToken(records.Field{Type: "number"}, nil)
	least, _ := records.SortToken(records.Field{Type: "number"}, json.RawMessage(`-1e308`))
	if none >= least {
		t.Errorf("no value %s does not sort before %s", none, least)
	}
}

func TestPrefixRangeHoldsTheTextsStartingWithIt(t *testing.T) {
	f := records.Field{Type: "text"}
	from, to := records.PrefixRange("Ap")
	for _, s := range []string{"ap", "AP", "apple", "Apricot", "a", "b", "aq", "ao", "zap"} {
		raw, _ := json.Marshal(s)
		tok, _ := records.SortToken(f, raw)
		in := tok >= from && tok < to
		if want := strings.HasPrefix(strings.ToLower(s), "ap"); in != want {
			t.Errorf("%q in range = %v, want %v", s, in, want)
		}
	}
}
