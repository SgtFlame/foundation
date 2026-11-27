// Package filesservice is the record files' data service (forge 21d-4):
// the tenants' FILES_<tenant> object stores, so a process (a connector
// type, a web process) may be allowed files without the live core. Its
// operations are for Go only: callers check access to the records first,
// as data.Upload and recordservice.OpenFile do.
package filesservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records"
)

type Service interface {
	// PutPart keeps one part (at most files.PartSize bytes) of an upload,
	// in the principal's tenant. Go only.
	//svc:idempotent
	PutPart(args PutPartArgs) PutPartResponse
	// Put stores an upload's parts as one file uploaded by the principal.
	// Go only.
	Put(args PutArgs) PutResponse
	// Uploader is who uploaded a file. Go only.
	//svc:idempotent
	Uploader(args UploaderArgs) UploaderResponse
	// Get reads a file: its reference, then its content in parts. Go only.
	//svc:stream
	Get(args GetArgs) *GetRequest
	// Delete removes a file. Go only.
	//svc:idempotent
	Delete(args DeleteArgs) DeleteResponse
}

type PutPartArgs struct {
	features.ServiceArgs
	Upload string // the upload's id, letters, digits, - and _
	Index  int
	Data   []byte
}

type PutPartResponse struct {
	Common features.ServiceResponse
}

type PutArgs struct {
	features.ServiceArgs
	Upload      string
	Parts       int
	Name        string
	ContentType string
	Max         int64 // the largest size taken: 413 file_too_large past it
}

type PutResponse struct {
	Common features.ServiceResponse
	File   records.FileRef
}

type UploaderArgs struct {
	features.ServiceArgs
	ID string
}

type UploaderResponse struct {
	Common features.ServiceResponse
	User   uint64 `json:",string"`
	Found  bool
}

type GetArgs struct {
	features.ServiceArgs // its context ends the read
	ID                   string
}

// GetPart is one event of a read: the file's reference first (or
// Missing), then its content in order, or the failure that ended it.
type GetPart struct {
	File    *records.FileRef          `json:"file,omitempty"`
	Missing bool                      `json:"missing,omitempty"`
	Data    []byte                    `json:"data,omitempty"`
	Failed  *features.ServiceResponse `json:"failed,omitempty"`
}

// GetRequest delivers the read; Parts closes when it ends.
type GetRequest struct {
	Parts <-chan GetPart
}

type DeleteArgs struct {
	features.ServiceArgs
	ID string
}

type DeleteResponse struct {
	Common features.ServiceResponse
}
