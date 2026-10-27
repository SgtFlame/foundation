package volumedto

import (
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type ListFolderResponse struct {
	rest.Status
	IsVolume bool          `json:"isVolume"`
	Folders  []FolderEntry `json:"folders"`
	Files    []FileEntry   `json:"files"`
}

// ListFolderResponseFrom converts the service's ListFolderResponse; its code is
// carried by the HTTP status rather than the body.
func ListFolderResponseFrom(resp volumeservice.ListFolderResponse) ListFolderResponse {
	folders := make([]FolderEntry, len(resp.Folders))
	for i, entry := range resp.Folders {
		folders[i] = FolderEntryFrom(entry)
	}
	files := make([]FileEntry, len(resp.Files))
	for i, entry := range resp.Files {
		files[i] = FileEntryFrom(entry)
	}
	return ListFolderResponse{
		Status:   rest.StatusFrom(resp.Common),
		IsVolume: resp.IsVolume,
		Folders:  folders,
		Files:    files,
	}
}
