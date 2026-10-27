package volumeservice

import "github.com/SgtFlame/foundation/backend/features/events"

// FileOp is what happened to a volume file.
type FileOp int

const (
	FileCreated FileOp = iota
	FileChanged
)

// FileEvent tells subscribers (such as the index) that a volume file was
// created or written.
type FileEvent struct {
	Op   FileOp
	File string // absolute path
}

// FileEvents carries FileEvents between the services that write volume files
// and those that react to them.
type FileEvents = *events.Bus[FileEvent]

func NewFileEvents() FileEvents {
	return events.NewBus[FileEvent]()
}
