package logsservice

import (
	"log/slog"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs"
)

// Service is a thin wrapper over VictoriaLogs' HTTP API
// (documentation/plans/Logging.md › Reading): it scopes every query to the
// principal's tenant unless they may read all logs, limits rows and time
// ranges, and turns LogsQL parse errors into 400s with their position. Stats
// and StatsRange come with the Dashboard (documentation/plans/Usage.md).
type Service interface {
	Status(args StatusArgs) StatusResponse
	Query(args QueryArgs) QueryResponse
	Hits(args HitsArgs) HitsResponse
	FieldNames(args FieldNamesArgs) FieldNamesResponse
	FieldValues(args FieldValuesArgs) FieldValuesResponse
	Streams(args StreamsArgs) StreamsResponse
	Tail(args TailArgs) *TailRequest
	Ingest(args IngestArgs) IngestResponse
	// SetUserLevel turns full logging for one user on (Level "debug") for
	// Hours, or off (Level ""). It needs logs.setUserLevel.
	SetUserLevel(args SetUserLevelArgs) SetUserLevelResponse
	// UserLevels lists the users with full logging on.
	UserLevels(args UserLevelsArgs) UserLevelsResponse
	// Level is the level the principal's browser forwards from: their own
	// while full logging is on, else the logging section's browserLevel.
	Level(args LevelArgs) LevelResponse
}

// DefaultUserLevelHours is how long full logging for one user lasts unless
// told otherwise, and MaxUserLevelHours the longest.
const (
	DefaultUserLevelHours = 24
	MaxUserLevelHours     = 7 * 24
)

type SetUserLevelArgs struct {
	features.ServiceArgs
	UserID uint64
	Level  string // "debug", or "" to turn it off
	Hours  int    // DefaultUserLevelHours when 0
}

type SetUserLevelResponse struct {
	Common features.ServiceResponse
	Level  *logs.UserLogLevel // nil once turned off
}

type UserLevelsArgs struct {
	features.ServiceArgs
}

type UserLevelsResponse struct {
	Common features.ServiceResponse
	Levels []logs.UserLogLevel
}

type LevelArgs struct {
	features.ServiceArgs
}

type LevelResponse struct {
	Common       features.ServiceResponse
	BrowserLevel slog.Level
	Until        time.Time // zero unless full logging is on
}

// Range is a query's time range. A zero End is now, and a zero Start an hour
// before End; it may span MaxRange at most.
type Range struct {
	Start, End time.Time
}

// MaxRange is the longest time range a query may span.
const MaxRange = 31 * 24 * time.Hour

// Row is one log record: VictoriaLogs returns every field as a string.
type Row map[string]string

// QueryError is where a LogsQL query failed to parse, for the editor to mark.
type QueryError struct {
	Message string
	Offset  int // in bytes from the start of the query, where parsing stopped
}

// ValueHits is a field name or value and how many records have it.
type ValueHits struct {
	Value string
	Hits  int64
}

type StatusArgs struct {
	features.ServiceArgs
}

// StatusResponse tells whether logs are stored and how storing is going.
type StatusResponse struct {
	Common       features.ServiceResponse
	Configured   bool
	URL          string // without credentials
	StoredLevel  slog.Level
	BrowserLevel slog.Level
	Queued       int
	Dropped      int64
	LastError    string
	LastErrorAt  time.Time
}

// QueryArgs runs a LogsQL query, newest records first.
type QueryArgs struct {
	features.ServiceArgs
	Range
	Query string
	Limit int // 500 when 0, at most 5000
}

type QueryResponse struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Rows       []Row
	Limited    bool // more records matched than Limit
}

// HitsArgs counts matching records per Step, split by Field's values when set.
type HitsArgs struct {
	features.ServiceArgs
	Range
	Query string
	Step  time.Duration // a 60th of the range when 0
	Field string
}

type HitsResponse struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Step       time.Duration
	Series     []HitsSeries
}

// HitsSeries is the count per step of the records with Fields.
type HitsSeries struct {
	Fields     map[string]string
	Timestamps []time.Time
	Values     []int64
	Total      int64
}

type FieldNamesArgs struct {
	features.ServiceArgs
	Range
	Query string
}

type FieldNamesResponse struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Names      []ValueHits
}

type FieldValuesArgs struct {
	features.ServiceArgs
	Range
	Query string
	Field string
	Limit int // 100 when 0, at most 1000
}

type FieldValuesResponse struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Values     []ValueHits
}

// StreamsArgs lists the streams, or with Field one stream field's values.
type StreamsArgs struct {
	features.ServiceArgs
	Range
	Query string
	Field string
}

type StreamsResponse struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Values     []ValueHits
}

// TailArgs follows new records matching Query until args' context ends.
type TailArgs struct {
	features.ServiceArgs
	Query string
}

// TailRequest delivers new records on Rows, which is closed when the tail
// ends. Common is the outcome of starting it: Rows is closed at once when it
// failed.
type TailRequest struct {
	Common     features.ServiceResponse
	QueryError *QueryError
	Rows       <-chan Row
}

// IngestArgs stores records forwarded from the principal's browser.
type IngestArgs struct {
	features.ServiceArgs
	Records []BrowserRecord
}

// BrowserRecord is a browser LogRecord (src/logging/LogRecord.ts).
type BrowserRecord struct {
	Time    time.Time
	Level   string
	Message string
	Domain  string
	Tags    []string
	Code    string
	Trace   string
	Span    string
	Hint    string
	Public  string
	Attrs   map[string]any
	Error   *BrowserError
	URL     string // the page it was logged on
}

type BrowserError struct {
	Name, Message, Stack string
	Cause                *BrowserError
}

type IngestResponse struct {
	Common   features.ServiceResponse
	Accepted int // records at or above the browser level, stored
}
