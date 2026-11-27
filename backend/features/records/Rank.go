package records

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Ranks are ordered positions (forge 30a, Records.md › Built in forge
// 30a): a rank is a base62 fraction, its digits after the point, written
// with the digits 0-9A-Za-z, which are in ASCII order, so ranks compare as
// text in the order of the fractions they write. A rank never ends in 0
// (so no two spellings write one fraction) and is never empty: there is
// always room for another rank between any two.

// RankDigits are a rank's digits in order.
const RankDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// MaxRank is how long a rank may be; RankRebalanceAt is the length past
// which a scope's ranks are written again evenly (record.rebalance).
const (
	MaxRank         = 256
	RankRebalanceAt = 64
)

// rankRoom is how many values a gap must hold at the length a new rank is
// written at: a new rank is drawn at random from the middle half of the
// gap, so two ranks computed between the same neighbours at once differ
// but for about one time in two thousand.
var rankRoom = big.NewInt(62 * 62)

// CheckRank reports why s is not a rank.
func CheckRank(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("a rank cannot be empty")
	case len(s) > MaxRank:
		return fmt.Errorf("a rank is at most %d characters", MaxRank)
	case s[len(s)-1] == '0':
		return fmt.Errorf("%q ends in 0, which no rank does", s)
	}
	for _, c := range []byte(s) {
		if strings.IndexByte(RankDigits, c) < 0 {
			return fmt.Errorf("%q holds %q, which is not one of 0-9, A-Z, a-z", s, c)
		}
	}
	return nil
}

// RankBetween is a new rank strictly between after and before: after empty
// is the start of the order, before empty its end, so RankBetween("", "")
// is a first rank. It is drawn at random from the middle half of the gap,
// so concurrent moves into one gap rarely tie (ties sort by record id).
// It fails when after is not below before or either is not a rank.
func RankBetween(after, before string) (string, error) {
	for _, r := range []string{after, before} {
		if r != "" {
			if err := CheckRank(r); err != nil {
				return "", err
			}
		}
	}
	if after != "" && before != "" && after >= before {
		return "", fmt.Errorf("%q is not before %q", after, before)
	}
	n := max(len(after), len(before), 1)
	lo, hi := rankInt(after, n), rankInt(before, n)
	if before == "" {
		hi = new(big.Int).Exp(big.NewInt(62), big.NewInt(int64(n)), nil)
	}
	gap := new(big.Int).Sub(hi, lo)
	for gap.Cmp(rankRoom) < 0 {
		n++
		lo.Mul(lo, big.NewInt(62))
		hi.Mul(hi, big.NewInt(62))
		gap.Mul(gap, big.NewInt(62))
	}
	quarter := new(big.Int).Rsh(gap, 2)
	jitter, err := rand.Int(rand.Reader, new(big.Int).Rsh(gap, 1))
	if err != nil {
		return "", err
	}
	x := lo.Add(lo, quarter)
	x.Add(x, jitter)
	return rankString(x, n), nil
}

// RankAmong is a rank between two neighbours' ranks, whatever has moved
// since they were shown: neighbours now in the other order give a rank
// between them still, and two with one rank give one just after it.
func RankAmong(after, before string) (string, error) {
	switch {
	case after != "" && before != "" && after == before:
		return RankBetween(after, after+"z")
	case after != "" && before != "" && after > before:
		after, before = before, after
	}
	return RankBetween(after, before)
}

// EvenRanks are n ranks spread evenly over the order, each as short as n
// allows: what record.rebalance writes.
func EvenRanks(n int) []string {
	if n <= 0 {
		return nil
	}
	digits := 1
	space := big.NewInt(62)
	for space.Cmp(big.NewInt(int64(n+1))) <= 0 {
		digits++
		space.Mul(space, big.NewInt(62))
	}
	step := new(big.Int).Div(space, big.NewInt(int64(n+1)))
	out := make([]string, n)
	x := new(big.Int)
	for i := range out {
		x.Add(x, step)
		out[i] = rankString(x, digits)
	}
	return out
}

// rankInt is a rank as an integer of n digits (n at least its length).
func rankInt(s string, n int) *big.Int {
	x := new(big.Int)
	for i := 0; i < n; i++ {
		x.Mul(x, big.NewInt(62))
		if i < len(s) {
			x.Add(x, big.NewInt(int64(strings.IndexByte(RankDigits, s[i]))))
		}
	}
	return x
}

// rankString writes x as n digits, without the trailing zeros.
func rankString(x *big.Int, n int) string {
	b := make([]byte, n)
	v := new(big.Int).Set(x)
	m := new(big.Int)
	for i := n - 1; i >= 0; i-- {
		v.DivMod(v, big.NewInt(62), m)
		b[i] = RankDigits[m.Int64()]
	}
	return strings.TrimRight(string(b), "0")
}

// rankOf converts a number to a rank in the same order (forge 30a: a
// number field becomes a rank field by its order); a text that is a rank
// was kept by Convert already.
func rankOf(from Field, v any) (json.RawMessage, error) {
	n, ok := v.(float64)
	if !ok {
		return nil, fmt.Errorf("%s is not a number or a rank", show(v))
	}
	bits := new(big.Int).SetBytes(floatBytes(n))
	if bits.Sign() == 0 {
		bits.SetInt64(1)
	}
	return json.Marshal(rankString(bits, 11)) // 62^11 > 2^64
}
