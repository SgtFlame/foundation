package recordservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// Service is the Go API over the records (documentation/Records.md › Built
// in slice 17): the record.* commands and the records.get and records.list
// queries, for other features, for REST clients outside the live
// connection, and for imports. Editors use the live connection itself.
type Service interface {
	Get(args GetArgs) GetResponse
	List(args ListArgs) ListResponse
	Import(args ImportArgs) ImportResponse
	// Create, Update and Delete are only called from Go.
	Create(args CreateArgs) CommandResponse
	Update(args UpdateArgs) CommandResponse
	Delete(args DeleteArgs) CommandResponse
}

type GetArgs struct {
	features.ServiceArgs
	Entity string
	ID     string
}

type GetResponse struct {
	Common features.ServiceResponse
	Record data.Record // nil when there is none
}

type ListArgs struct {
	features.ServiceArgs
	Entity string
	Where  map[string]json.RawMessage // field id → value the records must have
}

type ListResponse struct {
	Common  features.ServiceResponse
	Records []data.Record // oldest first
}

// ImportArgs are rows of field values, each made a record by its own
// record.create.
type ImportArgs struct {
	features.ServiceArgs
	Entity string
	Rows   []map[string]json.RawMessage
}

// RowProblems are why one row was not imported.
type RowProblems struct {
	Row      int
	Problems []live.Problem
}

type ImportResponse struct {
	Common  features.ServiceResponse
	Created int
	IDs     []string      // the new records, in row order; "" for a row that failed
	Failed  []RowProblems // the first 20 rows that failed
}

type CreateArgs struct {
	features.ServiceArgs
	Entity string
	ID     string // made when empty
	Values map[string]json.RawMessage
}

type UpdateArgs struct {
	features.ServiceArgs
	Entity string
	ID     string
	Set    map[string]json.RawMessage
	Expect uint64 // the record's _seq the change was decided on; 0 for any
}

type DeleteArgs struct {
	features.ServiceArgs
	Entity string
	ID     string
	Expect uint64
}

// CommandResponse is one command's commit.
type CommandResponse struct {
	Common   features.ServiceResponse
	ID       string // the record's id
	Seq      uint64
	Problems []live.Problem
}
