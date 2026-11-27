package recordsnodes

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/text/services/parseservice"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// ParseCSVID is Parse CSV's node type id (forge 40c).
const ParseCSVID = "records.parseCsv"

// ParseDeps are what Parse CSV needs, made after the registry.
type ParseDeps struct {
	// Parse is the parse service, from the locator (parseproxy.Use); without
	// it nothing is parsed
	Parse parseservice.Service
	// MaxRows is the records section's importMaxRows for the run's
	// principal; 50 000 when nil
	MaxRows func(args features.ServiceArgs) int
}

// ContributeParseCSV registers Parse CSV; deps gives its dependencies once
// made.
func ContributeParseCSV(reg *extensions.Registry, deps func() ParseDeps) {
	node(reg, 692, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: ParseCSVID}, Title: "Parse CSV",
		Description: "Reads a CSV file into columns and typed rows, for an import: a file the run's principal uploaded (a file field's value, or Upload file's) or pasted text. The delimiter (comma, semicolon, tab or bar), the header row and the encoding (UTF-8, UTF-16 with a byte order mark, Windows-1252) are detected unless given, and each column's type (number, decimal, date, dateTime, bool, email, url, else text) from its values. " +
			"Rows are keyed by column name, each value typed by its column (decimals as strings, dates as YYYY-MM-DD, dateTimes in UTC, empty cells null). sample returns only the first rows (a preview) while total counts them all; a file with more rows than the records section's importMaxRows (50 000) is refused. It never fails the run: OK is false and Error says why in plain words. Parsing runs in the parse service, which may run in another process.",
		Example:  "Parse CSV of the uploaded file with sample 20, then show its columns to map onto the entity type's fields",
		Examples: cognition.ConfigExample(ParseCSVID, `{"sample":20}`),
		Tags:     []string{"csv", "import", "spreadsheet", "upload", "parse", "rows", "columns", "tsv"},
		Kinds:    []string{livenodes.KindAction, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent, livenodes.KindTrigger},
		Sample: cognition.Values{"columns": []any{map[string]any{"name": "Email", "type": "email", "examples": []any{"ada@example.com"}}},
			"rows": []any{map[string]any{"Email": "ada@example.com"}}, "total": 1, "delimiter": ",", "header": true, "encoding": "utf-8", "name": "contacts.csv", "ok": true, "error": ""},
		Config: json.RawMessage(`{"type":"object","properties":{` +
			`"delimiter":{"type":"string","enum":["",",",";","\t","|"],"title":"Delimiter","description":"Detected when left empty"},` +
			`"header":{"type":"string","enum":["auto","yes","no"],"title":"Header row","description":"Whether the first row names the columns; detected when auto"},` +
			`"sample":{"type":"integer","minimum":0,"title":"Sample","description":"Rows to return (a preview); every row when 0"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				{ID: "file", Name: "File", Type: "json", Optional: true, Description: "An uploaded file as a file field holds it, {id, name, size, contentType}, or its id; read in place of Text"},
				{ID: "text", Name: "Text", Type: "string", Optional: true, Description: "Pasted CSV text, read when no file is given"},
				{ID: "sample", Name: "Sample", Type: "number", Optional: true, Description: "Rows to return, in place of the config's; every row when 0"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("columns", "Columns", "json", "The columns in order: [{name, type, examples}]"),
				cognition.DataPin("rows", "Rows", "json", "The rows (sample of them), each {column name: value}"),
				cognition.DataPin("total", "Total", "number", "How many data rows the file has"),
				cognition.DataPin("delimiter", "Delimiter", "string", "The delimiter used"),
				cognition.DataPin("header", "Header", "bool", "Whether the first row named the columns"),
				cognition.DataPin("encoding", "Encoding", "string", "utf-8, utf-16le, utf-16be or windows-1252"),
				cognition.DataPin("name", "Name", "string", "The file's name; empty for pasted text"),
				cognition.DataPin("ok", "OK", "bool", "Whether the file was read"),
				cognition.DataPin("error", "Error", "string", "Why it was not read, in plain words; empty when OK"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			out := cognition.Values{"columns": []any{}, "rows": []any{}, "total": 0, "delimiter": "", "header": false, "encoding": "", "name": "", "ok": false, "error": ""}
			fail := func(msg string) (cognition.Values, string, error) {
				out["error"] = msg
				return out, cognition.Then, nil
			}
			var c struct {
				Delimiter, Header string
				Sample            int
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			var d ParseDeps
			if deps != nil {
				d = deps()
			}
			if d.Parse == nil {
				return fail("CSV files cannot be parsed here")
			}
			a := parseservice.ParseCSVArgs{ServiceArgs: x.ServiceArgs, Delimiter: c.Delimiter, Sample: c.Sample}
			if in["sample"] != nil {
				if n, err := cognition.Get[float64](in, "sample"); err == nil {
					a.Sample = int(n)
				}
			}
			switch c.Header {
			case "yes":
				yes := true
				a.Header = &yes
			case "no":
				a.Header = new(bool)
			}
			switch f := in["file"].(type) {
			case string:
				a.File = f
			case map[string]any:
				a.File, _ = f["id"].(string)
			}
			a.Text, _ = cognition.Get[string](in, "text")
			if a.File == "" && a.Text == "" {
				return fail("no file or text was given")
			}
			if d.MaxRows != nil {
				a.MaxRows = d.MaxRows(x.ServiceArgs)
			}
			res := d.Parse.ParseCSV(a)
			if res.Common.Code >= 300 {
				msg := res.Common.Message
				if msg == "" {
					msg = "the file could not be parsed"
				}
				return fail(msg)
			}
			cols, rows := plain(res.Columns), plain(res.Rows)
			if rows == nil {
				rows = []any{}
			}
			if cols == nil {
				cols = []any{}
			}
			out["columns"], out["rows"], out["total"] = cols, rows, res.Total
			out["delimiter"], out["header"], out["encoding"], out["name"], out["ok"] = res.Delimiter, res.Header, res.Encoding, res.Name, true
			return out, cognition.Then, nil
		},
	})
}
