package records

import (
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Values are ordered by their sort tokens (Forge's part 3d): a value's
// bytes in an order-preserving form, written in base32hex, whose alphabet
// is in ASCII order, so tokens compare as their values do. A token is one
// KV key part, so an index entry is <entity>.<index>.<token>….<recordId>;
// '.' sorts below every token character, so a shorter token sorts first.
// Queries that scan sort by the same tokens, so an index and a scan agree.

// NullToken is the token of a field without a value: it sorts first.
var NullToken = Token([]byte{0})

// textBytes is how much of a text an index keeps, twice: folded to lower
// case, then as written. Texts that agree on that much sort by record id.
const textBytes = 120

var tokens = base32.HexEncoding.WithPadding(base32.NoPadding)

// Token is bytes as one sort token.
func Token(b []byte) string { return tokens.EncodeToString(b) }

// Sortable reports whether a field's values have an order: whether it
// can be indexed, filtered by range and sorted by.
func Sortable(f Field) bool {
	switch f.Type {
	case "text", "longText", "number", "decimal", "money", "bool", "date", "dateTime", "duration", "enum", "relation", "user":
		return f.Type != "relation" || f.Relation == nil || !f.Relation.Many
	}
	return false
}

// Indexable reports whether a field can be in an index: a sortable field,
// or a list-like one whose items are (indexed once per item).
func Indexable(f Field) bool {
	if item, many := ItemOf(f); many {
		return Sortable(item)
	}
	return Sortable(f)
}

// SortTokens are a stored value's tokens: one for a value (NullToken when
// there is none), and one per item, distinct, for a list-like field (just
// NullToken when it is empty).
func SortTokens(f Field, raw json.RawMessage) ([]string, error) {
	if IsNull(raw) {
		return []string{NullToken}, nil
	}
	item, many := ItemOf(f)
	if !many {
		b, err := sortBytes(f, raw)
		if err != nil {
			return nil, err
		}
		return []string{Token(b)}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := []string{}
	for _, it := range items {
		b, err := sortBytes(item, it)
		if err != nil {
			return nil, err
		}
		if t := Token(b); !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return []string{NullToken}, nil
	}
	return out, nil
}

// SortToken is the token of one value of a sortable field.
func SortToken(f Field, raw json.RawMessage) (string, error) {
	if IsNull(raw) {
		return NullToken, nil
	}
	b, err := sortBytes(f, raw)
	if err != nil {
		return "", err
	}
	return Token(b), nil
}

// PrefixRange is the tokens between which every text starting with p (in
// any case) sorts: from inclusive, to exclusive.
func PrefixRange(p string) (from, to string) {
	b := append([]byte{1}, lowerCut(p)...)
	next := slices.Clone(b)
	for i := len(next) - 1; i >= 0; i-- { // UTF-8 never holds 0xFF, so no carry runs out
		if next[i] < 0xFF {
			next[i]++
			next = next[:i+1]
			break
		}
	}
	return Token(b), Token(next)
}

// sortBytes is a value in its order-preserving form: 1, then the value's
// bytes (0 alone is no value).
func sortBytes(f Field, raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out := []byte{1}
	switch f.Type {
	case "text", "longText":
		s, ok := v.(string)
		if !ok {
			return nil, want("text", v)
		}
		out = append(out, lowerCut(s)...)
		out = append(out, 0)
		return append(out, cut(s, textBytes)...), nil
	case "number":
		n, ok := v.(float64)
		if !ok {
			return nil, want("a number", v)
		}
		return append(out, floatBytes(n)...), nil
	case "decimal":
		s, ok := v.(string)
		if !ok {
			return nil, want("a decimal", v)
		}
		return decimalBytes(out, s)
	case "money":
		if s, ok := v.(string); ok {
			return decimalBytes(out, s)
		}
		m, _ := v.(map[string]any)
		c, _ := m["currency"].(string)
		a, _ := m["amount"].(string)
		return decimalBytes(append(out, c...), a)
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, want("true or false", v)
		}
		if b {
			return append(out, 1), nil
		}
		return append(out, 0), nil
	case "date":
		s, _ := v.(string)
		if _, err := time.Parse(time.DateOnly, s); err != nil {
			return nil, want("a date", v)
		}
		return append(out, s...), nil
	case "dateTime":
		s, _ := v.(string)
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, want("a time", v)
		}
		out = binary.BigEndian.AppendUint64(out, uint64(t.Unix())^(1<<63))
		return binary.BigEndian.AppendUint32(out, uint32(t.Nanosecond())), nil
	case "duration":
		s, _ := v.(string)
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, want("a duration", v)
		}
		return binary.BigEndian.AppendUint64(out, uint64(d)^(1<<63)), nil
	case "enum":
		// Options sort in the order the field lists them
		s, _ := v.(string)
		i := slices.IndexFunc(f.Options, func(o Option) bool { return o.ID == s })
		if i < 0 {
			return append(binary.BigEndian.AppendUint16(out, math.MaxUint16), s...), nil
		}
		return binary.BigEndian.AppendUint16(out, uint16(i)), nil
	case "relation", "user":
		s, ok := v.(string)
		if !ok {
			return nil, want("an id", v)
		}
		return append(out, s...), nil
	}
	return nil, fmt.Errorf("%s values have no order", f.Type)
}

// floatBytes orders floats: positive ones with the sign bit set, negative
// ones with every bit flipped.
func floatBytes(n float64) []byte {
	if n == 0 {
		n = 0 // -0 sorts with 0
	}
	bits := math.Float64bits(n)
	if bits&(1<<63) != 0 {
		bits = ^bits
	} else {
		bits |= 1 << 63
	}
	return binary.BigEndian.AppendUint64(nil, bits)
}

// decimalBytes orders exact decimals of any scale: a sign byte (0
// negative, 1 zero, 2 positive), the exponent (where the point is,
// biased), then the significant digits without trailing zeros. A negative
// number has its exponent and digits flipped and ends in 0xFF, so a longer
// one (further from zero) sorts first.
func decimalBytes(out []byte, s string) ([]byte, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal", s)
	}
	if r.Sign() == 0 {
		return append(out, 1), nil
	}
	neg := r.Sign() < 0
	digits := strings.TrimPrefix(s, "-")
	digits = strings.TrimPrefix(digits, "+")
	point := strings.IndexByte(digits, '.')
	if point < 0 {
		point = len(digits)
	}
	digits = strings.Replace(digits, ".", "", 1)
	lead := len(digits) - len(strings.TrimLeft(digits, "0"))
	digits = strings.TrimRight(digits[lead:], "0")
	exp := uint16(point - lead + 0x8000)
	if neg {
		out = append(out, 0)
		out = binary.BigEndian.AppendUint16(out, ^exp)
		for _, d := range []byte(digits) {
			out = append(out, '9'-d+'0')
		}
		return append(out, 0xFF), nil
	}
	out = append(out, 2)
	out = binary.BigEndian.AppendUint16(out, exp)
	return append(out, digits...), nil
}

func lowerCut(s string) []byte { return cut(strings.ToLower(s), textBytes) }

// cut is at most n bytes of s, ending on a whole character.
func cut(s string, n int) []byte {
	if len(s) <= n {
		return []byte(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return []byte(s[:n])
}
