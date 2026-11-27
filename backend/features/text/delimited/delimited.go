// Package delimited reads CSV and other delimited text into typed rows
// (forge 40c, Parse CSV): the encoding (UTF-8, UTF-16 with a byte order
// mark, else Windows-1252), the delimiter (comma, semicolon, tab or bar)
// and the header row are detected when not given, and each column's type
// (number, decimal, date, dateTime, bool, email, url, else text) from its
// values. It runs in the server process because Go's encoding/csv runs no
// decoder of complex formats; sizes, rows, columns and fields are bounded.
// parseservice is its only caller, so it can move out of process.
package delimited

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Column types, as records field types name them.
const (
	Text     = "text"
	Number   = "number"
	Decimal  = "decimal"
	Date     = "date"
	DateTime = "dateTime"
	Bool     = "bool"
	Email    = "email"
	URL      = "url"
)

// Options bound and steer a parse; zero values take the defaults.
type Options struct {
	Delimiter string // ",", ";", "\t" or "|"; detected when empty
	Header    *bool  // whether the first row names the columns; detected when nil
	Sample    int    // rows returned; 0 for every row
	MaxRows   int    // data rows allowed, DefaultMaxRows when 0
	MaxBytes  int    // DefaultMaxBytes when 0
	MaxCols   int    // DefaultMaxCols when 0
	MaxField  int    // bytes in one field, DefaultMaxField when 0
}

// The limits' defaults.
const (
	DefaultMaxRows  = 50000
	DefaultMaxBytes = 64 << 20
	DefaultMaxCols  = 500
	DefaultMaxField = 64 << 10
)

// Column is one column: its name (from the header, else Column n, made
// unique), its detected type and up to three example values as written.
type Column struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Examples []string `json:"examples"`
}

// Result is a parse: the columns, the rows (Sample of them, each by column
// name, typed by its column: numbers as numbers, decimals as strings with
// their digits, dates as YYYY-MM-DD, dateTimes as RFC 3339 in UTC, bools,
// and text; nil for an empty cell), how many data rows there are, and
// what was detected.
type Result struct {
	Columns   []Column         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Total     int              `json:"total"`
	Delimiter string           `json:"delimiter"`
	Header    bool             `json:"header"`
	Encoding  string           `json:"encoding"`
}

// Error is a refused parse: a code and words that name nothing private.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func refuse(code, format string, a ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Parse reads data.
func Parse(data []byte, o Options) (Result, error) {
	if o.MaxRows <= 0 {
		o.MaxRows = DefaultMaxRows
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.MaxCols <= 0 {
		o.MaxCols = DefaultMaxCols
	}
	if o.MaxField <= 0 {
		o.MaxField = DefaultMaxField
	}
	if len(data) > o.MaxBytes {
		return Result{}, refuse("file_too_large", "the file is %d bytes, more than the %d a CSV import takes", len(data), o.MaxBytes)
	}
	text, encoding, err := decode(data)
	if err != nil {
		return Result{}, err
	}
	delim := o.Delimiter
	if delim == "" {
		delim = detectDelimiter(text)
	}
	if !validDelimiter(delim) {
		return Result{}, refuse("bad_delimiter", "%q is not a delimiter: use a comma, a semicolon, a tab or a bar", delim)
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = []rune(delim)[0]
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var raw [][]string
	width := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line, _ := r.FieldPos(0)
			return Result{}, refuse("bad_csv", "the file is not CSV near line %d: %v", line, unwrapCSV(err))
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue // a blank line
		}
		if len(rec) > o.MaxCols {
			return Result{}, refuse("too_many_columns", "a row has %d columns, more than the %d allowed", len(rec), o.MaxCols)
		}
		for _, f := range rec {
			if len(f) > o.MaxField {
				return Result{}, refuse("field_too_large", "a field holds %d bytes, more than the %d allowed", len(f), o.MaxField)
			}
		}
		// The header row and at most MaxRows data rows
		if len(raw) > o.MaxRows {
			return Result{}, refuse("too_many_rows", "the file has more than %d rows; split it, or raise the records section's importMaxRows", o.MaxRows)
		}
		raw = append(raw, rec)
		width = max(width, len(rec))
	}
	if len(raw) == 0 {
		return Result{Columns: []Column{}, Rows: []map[string]any{}, Delimiter: delim, Header: false, Encoding: encoding}, nil
	}
	for i := range raw {
		for len(raw[i]) < width {
			raw[i] = append(raw[i], "")
		}
	}
	header := false
	if o.Header != nil {
		header = *o.Header
	} else {
		header = detectHeader(raw, width)
	}
	body := raw
	if header {
		body = raw[1:]
	}
	if len(body) > o.MaxRows {
		return Result{}, refuse("too_many_rows", "the file has %d rows, more than the %d allowed; split it, or raise the records section's importMaxRows", len(body), o.MaxRows)
	}
	var names []string
	if header {
		names = uniqueNames(raw[0], width)
	} else {
		names = uniqueNames(nil, width)
	}
	cols := make([]Column, width)
	readers := make([]reader, width)
	for c := range width {
		values := make([]string, len(body))
		for i, row := range body {
			values[i] = row[c]
		}
		typ, rd := detectType(values)
		cols[c] = Column{Name: names[c], Type: typ, Examples: examples(values)}
		readers[c] = rd
	}
	n := len(body)
	if o.Sample > 0 && o.Sample < n {
		n = o.Sample
	}
	rows := make([]map[string]any, n)
	for i := range n {
		row := make(map[string]any, width)
		for c := range width {
			row[names[c]] = readers[c](strings.TrimSpace(body[i][c]))
		}
		rows[i] = row
	}
	return Result{Columns: cols, Rows: rows, Total: len(body), Delimiter: delim, Header: header, Encoding: encoding}, nil
}

func unwrapCSV(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func validDelimiter(d string) bool {
	return d == "," || d == ";" || d == "\t" || d == "|"
}

// decode turns the file into text: UTF-8 (a byte order mark dropped),
// UTF-16 with a byte order mark, else Windows-1252.
func decode(b []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
		if !utf8.Valid(b) {
			return "", "", refuse("bad_encoding", "the file says it is UTF-8 but is not")
		}
		return string(b), "utf-8", nil
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return utf16Text(b[2:], false), "utf-16le", nil
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return utf16Text(b[2:], true), "utf-16be", nil
	case utf8.Valid(b):
		if bytes.IndexByte(b, 0) >= 0 {
			return "", "", refuse("bad_encoding", "the file holds binary data, not text")
		}
		return string(b), "utf-8", nil
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return "", "", refuse("bad_encoding", "the file holds binary data, not text")
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c >= 0x80 && c <= 0x9F {
			sb.WriteRune(cp1252[c-0x80])
		} else {
			sb.WriteRune(rune(c))
		}
	}
	return sb.String(), "windows-1252", nil
}

// cp1252 is Windows-1252's 0x80 to 0x9F; the rest is Latin-1.
var cp1252 = [32]rune{'€', '\u0081', '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', '\u008D', 'Ž', '\u008F',
	'\u0090', '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', '\u009D', 'ž', 'Ÿ'}

func utf16Text(b []byte, big bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if big {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

// detectDelimiter picks the candidate that splits the first rows into the
// most columns consistently.
func detectDelimiter(text string) string {
	best, bestScore := ",", 0
	for _, d := range []string{",", ";", "\t", "|"} {
		r := csv.NewReader(strings.NewReader(text))
		r.Comma = []rune(d)[0]
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		counts := map[int]int{}
		rows := 0
		for rows < 20 {
			rec, err := r.Read()
			if err != nil {
				break
			}
			if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
				continue
			}
			counts[len(rec)]++
			rows++
		}
		// The most common width, weighted by how many rows have it
		for width, n := range counts {
			if width < 2 {
				continue
			}
			if score := n * width; n*2 > rows && score > bestScore {
				best, bestScore = d, score
			}
		}
	}
	return best
}

// detectHeader is true when the first row names the columns: some column
// whose other values have a type holds text in the first row, or every
// column is text and the first row's values are filled and unique.
func detectHeader(raw [][]string, width int) bool {
	if len(raw) < 2 {
		return allText(raw[0])
	}
	allTextCols := true
	for c := range width {
		values := make([]string, 0, len(raw)-1)
		for _, row := range raw[1:] {
			values = append(values, row[c])
		}
		typ, _ := detectType(values)
		first := strings.TrimSpace(raw[0][c])
		if typ != Text {
			allTextCols = false
			if first != "" {
				if t, _ := detectType([]string{first}); t != typ && !(typ == Decimal && t == Number) {
					return true
				}
			}
		}
	}
	if !allTextCols {
		return false
	}
	seen := map[string]bool{}
	for _, v := range raw[0] {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

func allText(row []string) bool {
	for _, v := range row {
		if t, _ := detectType([]string{v}); t != Text {
			return false
		}
	}
	return true
}

func uniqueNames(header []string, width int) []string {
	out := make([]string, width)
	used := map[string]int{}
	for c := range width {
		name := ""
		if c < len(header) {
			name = strings.TrimSpace(header[c])
		}
		if name == "" {
			name = fmt.Sprintf("Column %d", c+1)
		}
		base := name
		for used[strings.ToLower(name)] > 0 {
			used[strings.ToLower(base)]++
			name = fmt.Sprintf("%s %d", base, used[strings.ToLower(base)])
		}
		used[strings.ToLower(name)]++
		out[c] = name
	}
	return out
}

func examples(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// reader turns a trimmed cell into its column's value.
type reader func(string) any

var (
	intRe     = regexp.MustCompile(`^[-+]?\d+$`)
	decRe     = regexp.MustCompile(`^[-+]?(\d+\.\d*|\.\d+)$`)
	groupedRe = regexp.MustCompile(`^[-+]?\d{1,3}(,\d{3})+(\.\d+)?$`)
	moneyRe   = regexp.MustCompile(`^[-+]?[$€£¥]\s?`)
	emailRe   = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	urlRe     = regexp.MustCompile(`^(?i)https?://[^\s]+$`)
)

// dateLayouts are tried in order; the slash layouts' day and month order
// is decided for the whole column.
var dateLayouts = []string{"2006-01-02", "2006/01/02", "Jan 2, 2006", "January 2, 2006", "2 Jan 2006", "2 January 2006", "02-Jan-2006", "2-Jan-2006", "02.01.2006", "2.1.2006"}

var dateTimeLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05 -0700"}

// numberText is a cell as a plain number's text (grouping and a currency
// sign dropped), and whether it is one; frac tells a decimal point.
func numberText(v string) (string, bool, bool) {
	neg := ""
	if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
		if v[0] == '-' {
			neg = "-"
		}
		v = v[1:]
	}
	v = moneyRe.ReplaceAllString(v, "")
	if groupedRe.MatchString(v) {
		v = strings.ReplaceAll(v, ",", "")
	}
	switch {
	case intRe.MatchString(v):
		return neg + strings.TrimPrefix(v, "+"), true, false
	case decRe.MatchString(v):
		if strings.HasPrefix(v, ".") {
			v = "0" + v
		}
		return neg + v, true, true
	}
	return "", false, false
}

func parseBool(v string) (bool, bool) {
	switch strings.ToLower(v) {
	case "true", "yes":
		return true, true
	case "false", "no":
		return false, true
	}
	return false, false
}

func parseDate(v string, dayFirst bool) (time.Time, bool) {
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	slash := []string{"1/2/2006", "01/02/2006", "1/2/06"}
	if dayFirst {
		slash = []string{"2/1/2006", "02/01/2006", "2/1/06"}
	}
	for _, l := range slash {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseDateTime(v string, dayFirst bool) (time.Time, bool) {
	for _, l := range dateTimeLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	layouts := []string{"1/2/2006 15:04:05", "1/2/2006 15:04", "1/2/2006 3:04 PM", "1/2/2006 3:04:05 PM"}
	if dayFirst {
		layouts = []string{"2/1/2006 15:04:05", "2/1/2006 15:04", "2/1/2006 3:04 PM", "2/1/2006 3:04:05 PM"}
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// dayFirst is whether a column's slash dates put the day first: some value
// whose first part cannot be a month.
func dayFirst(values []string) bool {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if i := strings.IndexByte(v, '/'); i > 0 && i <= 2 {
			if n, err := strconv.Atoi(v[:i]); err == nil && n > 12 {
				return true
			}
		}
	}
	return false
}

// detectType is the most specific type every filled value has, with the
// reader for it.
func detectType(values []string) (string, reader) {
	filled := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			filled = append(filled, v)
		}
	}
	text := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	if len(filled) == 0 {
		return Text, text
	}
	all := func(ok func(string) bool) bool {
		for _, v := range filled {
			if !ok(v) {
				return false
			}
		}
		return true
	}
	if all(func(v string) bool { _, ok := parseBool(v); return ok }) {
		return Bool, func(v string) any {
			if b, ok := parseBool(v); ok {
				return b
			}
			return nil
		}
	}
	if all(func(v string) bool { _, ok, frac := numberText(v); return ok && !frac }) {
		return Number, func(v string) any {
			t, ok, _ := numberText(v)
			if !ok {
				return nil
			}
			if n, err := strconv.ParseInt(t, 10, 64); err == nil && (n < 1<<53 && n > -(1<<53)) {
				return n
			}
			return t // too large for a JSON number: kept exact as text
		}
	}
	if all(func(v string) bool { _, ok, _ := numberText(v); return ok }) {
		return Decimal, func(v string) any {
			if t, ok, _ := numberText(v); ok {
				return t
			}
			return nil
		}
	}
	df := dayFirst(filled)
	if all(func(v string) bool { _, ok := parseDate(v, df); return ok }) {
		return Date, func(v string) any {
			if t, ok := parseDate(v, df); ok {
				return t.Format("2006-01-02")
			}
			return nil
		}
	}
	if all(func(v string) bool {
		if _, ok := parseDateTime(v, df); ok {
			return true
		}
		_, ok := parseDate(v, df)
		return ok
	}) {
		return DateTime, func(v string) any {
			if t, ok := parseDateTime(v, df); ok {
				return t.UTC().Format(time.RFC3339)
			}
			if t, ok := parseDate(v, df); ok {
				return t.UTC().Format(time.RFC3339)
			}
			return nil
		}
	}
	if all(emailRe.MatchString) {
		return Email, text
	}
	if all(urlRe.MatchString) {
		return URL, text
	}
	return Text, text
}
