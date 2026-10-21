package volumedto

import "github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"

type FolderEntry struct {
	Name     string `json:"name"`
	IsVolume bool   `json:"isVolume"`
}

func FolderEntryFrom(entry volumeservice.FolderEntry) FolderEntry {
	return FolderEntry{Name: entry.Name, IsVolume: entry.IsVolume}
}
