package delimited

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func parse(t *testing.T, s string, o Options) Result {
	t.Helper()
	r, err := Parse([]byte(s), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func types(r Result) string {
	var out []string
	for _, c := range r.Columns {
		out = append(out, c.Name+":"+c.Type)
	}
	return strings.Join(out, " ")
}

func TestTypesAndHeader(t *testing.T) {
	r := parse(t, "Name,Email,Amount,Price,Joined,Seen,Active,Site\n"+
		"Ada,ada@example.com,12,\"1,200.50\",2026-01-15,2026-01-15 09:30,yes,https://ada.dev\n"+
		"Bob,bob@example.com,-3,$7.25,2026-02-01,2026-02-01T10:00:00Z,no,http://bob.example\n", Options{})
	if !r.Header || r.Delimiter != "," || r.Encoding != "utf-8" || r.Total != 2 {
		t.Fatalf("detected %+v", r)
	}
	if got := types(r); got != "Name:text Email:email Amount:number Price:decimal Joined:date Seen:dateTime Active:bool Site:url" {
		t.Fatalf("types %s", got)
	}
	want := map[string]any{"Name": "Ada", "Email": "ada@example.com", "Amount": int64(12), "Price": "1200.50", "Joined": "2026-01-15",
		"Seen": "2026-01-15T09:30:00Z", "Active": true, "Site": "https://ada.dev"}
	if !reflect.DeepEqual(r.Rows[0], want) {
		t.Fatalf("row %#v", r.Rows[0])
	}
	if r.Rows[1]["Price"] != "7.25" || r.Rows[1]["Amount"] != int64(-3) {
		t.Fatalf("row 2 %#v", r.Rows[1])
	}
	if !reflect.DeepEqual(r.Columns[0].Examples, []string{"Ada", "Bob"}) {
		t.Fatalf("examples %v", r.Columns[0].Examples)
	}
}

func TestSemicolonsQuotedNewlinesAndBOM(t *testing.T) {
	r := parse(t, "\xef\xbb\xbfname;note\n\"Ada\";\"line one\nline two; still\"\nBob;\n", Options{})
	if r.Delimiter != ";" || !r.Header || r.Total != 2 {
		t.Fatalf("detected %+v", r)
	}
	if r.Rows[0]["note"] != "line one\nline two; still" || r.Rows[1]["note"] != nil || r.Columns[0].Name != "name" {
		t.Fatalf("rows %#v", r.Rows)
	}
}

func TestTabsNoHeaderAndDuplicates(t *testing.T) {
	r := parse(t, "1\t2026-03-04\n2\t2026-03-05\n", Options{})
	if r.Delimiter != "\t" || r.Header || types(r) != "Column 1:number Column 2:date" {
		t.Fatalf("detected %+v %s", r, types(r))
	}
	r = parse(t, "a,A,,b\nx,y,z,w\n", Options{Header: new(bool)})
	if r.Header || r.Total != 2 {
		t.Fatalf("header forced off: %+v", r)
	}
	yes := true
	r = parse(t, "a,A,,b\nx,y,z,w\n", Options{Header: &yes})
	if got := types(r); got != "a:text A 2:text Column 3:text b:text" {
		t.Fatalf("names %s", got)
	}
}

func TestDayFirstDates(t *testing.T) {
	r := parse(t, "when\n03/04/2026\n25/12/2026\n", Options{})
	if r.Columns[0].Type != Date || r.Rows[0]["when"] != "2026-04-03" || r.Rows[1]["when"] != "2026-12-25" {
		t.Fatalf("%+v", r)
	}
	r = parse(t, "when\n03/04/2026\n12/25/2026\n", Options{})
	if r.Rows[0]["when"] != "2026-03-04" {
		t.Fatalf("month first %+v", r.Rows)
	}
}

func TestWindows1252AndUTF16(t *testing.T) {
	// "Café,Zoë" then "naïve,€5" in Windows-1252
	b := []byte("name,price\nCaf\xe9,\x805\nZo\xeb,\x806\n")
	r, err := Parse(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Encoding != "windows-1252" || r.Rows[0]["name"] != "Café" || r.Rows[1]["name"] != "Zoë" || r.Columns[1].Type != Number || r.Rows[0]["price"] != int64(5) {
		t.Fatalf("%+v", r)
	}
	u := utf16.Encode([]rune("name,n\nÅsa,1\n"))
	le := []byte{0xFF, 0xFE}
	for _, x := range u {
		le = append(le, byte(x), byte(x>>8))
	}
	r, err = Parse(le, Options{})
	if err != nil || r.Encoding != "utf-16le" || r.Rows[0]["name"] != "Åsa" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestSampleAndLimits(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("n\n")
	for i := range 50001 {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	_, err := Parse([]byte(sb.String()), Options{})
	var e *Error
	if !errors.As(err, &e) || e.Code != "too_many_rows" {
		t.Fatalf("50 001 rows: %v", err)
	}
	r, err := Parse([]byte(sb.String()), Options{MaxRows: 60000, Sample: 20})
	if err != nil || r.Total != 50001 || len(r.Rows) != 20 {
		t.Fatalf("sample: %v %d %d", err, r.Total, len(r.Rows))
	}
	if _, err := Parse([]byte("a,b\n"+strings.Repeat("x", 100)+",1\n"), Options{MaxField: 50}); !errors.As(err, &e) || e.Code != "field_too_large" {
		t.Fatalf("field: %v", err)
	}
	if _, err := Parse([]byte("a\x00b"), Options{}); !errors.As(err, &e) || e.Code != "bad_encoding" {
		t.Fatalf("binary: %v", err)
	}
	if _, err := Parse([]byte("abc"), Options{MaxBytes: 2}); !errors.As(err, &e) || e.Code != "file_too_large" {
		t.Fatalf("size: %v", err)
	}
	if _, err := Parse([]byte("a,b"), Options{Delimiter: "x"}); !errors.As(err, &e) || e.Code != "bad_delimiter" {
		t.Fatalf("delimiter: %v", err)
	}
	r = parse(t, "", Options{})
	if len(r.Columns) != 0 || r.Total != 0 {
		t.Fatalf("empty %+v", r)
	}
}
