// Package parseservice parses files people upload into data (forge 40c):
// CSV first (ParseCSV, for Parse CSV and the CSV import wizard), later XLSX,
// DOCX and transcripts. Callers reach it only through the locator
// (parseproxy.Use), so it runs in process by default and serve --services
// can move it to its own process or machine; an operation may also run its
// parser in a limited child process of its own.
package parseservice

import (
	"github.com/SgtFlame/foundation/backend/features"
)

type Service interface {
	// ParseCSV reads a CSV (or semicolon, tab or bar delimited) file the
	// principal uploaded, or text given inline, into columns with detected
	// types and typed rows. Go only.
	//svc:idempotent
	ParseCSV(args ParseCSVArgs) ParseCSVResponse
}

type ParseCSVArgs struct {
	features.ServiceArgs
	// File is an uploaded file's id (a file field's {id}); the principal
	// must have uploaded it (a service principal may read any)
	File string
	// Text is the content when no file is named
	Text      string
	Delimiter string // ",", ";", "\t" or "|"; detected when empty
	Header    *bool  // whether the first row names the columns; detected when nil
	Sample    int    // rows returned; every row when 0
	MaxRows   int    // data rows allowed (the records section's importMaxRows); 50 000 when 0
}

// Column is one column: its name, its detected type (text, number,
// decimal, date, dateTime, bool, email or url) and up to three examples.
type Column struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Examples []string `json:"examples"`
}

type ParseCSVResponse struct {
	Common    features.ServiceResponse
	Columns   []Column
	Rows      []map[string]any // by column name, typed by the column
	Total     int              // data rows in the file
	Delimiter string
	Header    bool
	Encoding  string // utf-8, utf-16le, utf-16be or windows-1252
	Name      string // the file's name, when a file was read
}
