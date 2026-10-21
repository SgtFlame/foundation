package volumeservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Service navigates the knowledge base root and turns folders into volumes.
type Service interface {
	ListFolder(args ListFolderArgs) ListFolderResponse
	MakeFolder(args MakeFolderArgs) MakeFolderResponse
	RemoveFolder(args RemoveFolderArgs) RemoveFolderResponse
	CreateVolume(args CreateVolumeArgs) CreateVolumeResponse
	CreateFile(args CreateFileArgs) CreateFileResponse
}

// FileTypes are the content types CreateFile accepts.
var FileTypes = []string{"markdown"}

type ListFolderArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath
}

type FolderEntry struct {
	Name     string
	IsVolume bool
}

type FileEntry struct {
	Name string // without the extension
	Type string // content type, e.g. "markdown"
}

type ListFolderResponse struct {
	Common   features.ServiceResponse
	IsVolume bool // whether the listed folder itself is a volume
	Folders  []FolderEntry
	Files    []FileEntry
}

type MakeFolderArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath
}

type MakeFolderResponse struct {
	Common features.ServiceResponse
}

type RemoveFolderArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath
}

type RemoveFolderResponse struct {
	Common features.ServiceResponse
}

type CreateVolumeArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath
}

type CreateVolumeResponse struct {
	Common   features.ServiceResponse
	VolumeID uint64
}

type CreateFileArgs struct {
	features.ServiceArgs
	Path kbservice.VolumePath // the folders down to the file's name, without its extension
	Type string               // one of FileTypes
}

type CreateFileResponse struct {
	Common features.ServiceResponse
}
