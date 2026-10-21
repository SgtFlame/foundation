package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type FileEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func FileEntryFrom(entry volumeservice.FileEntry) FileEntry {
	return FileEntry{Name: entry.Name, Type: entry.Type}
}
