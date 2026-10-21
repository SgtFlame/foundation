package impl

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// ListFolder lists the subfolders and files of args.Path, sorted by name.
// Folders that cannot be used as path folders (hidden or dotted names) and
// files of unknown types are skipped.
func (s *service) ListFolder(args volumeservice.ListFolderArgs) volumeservice.ListFolderResponse {
	dir, failed := s.resolve(args.Path, false)
	if failed == nil {
		failed = s.statFolder(dir)
	}
	if failed != nil {
		return volumeservice.ListFolderResponse{Common: *failed}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return volumeservice.ListFolderResponse{Common: *fail(http.StatusInternalServerError, "%s", err)}
	}

	folders := []volumeservice.FolderEntry{}
	files := []volumeservice.FileEntry{}
	for _, entry := range entries {
		switch {
		case entry.IsDir():
			if kbservice.ValidateFolder(entry.Name()) != nil {
				continue
			}
			folders = append(folders, volumeservice.FolderEntry{
				Name:     entry.Name(),
				IsVolume: isVolume(filepath.Join(dir, entry.Name())),
			})
		case entry.Type().IsRegular():
			if base, fileType, ok := kbservice.SplitFile(entry.Name()); ok {
				files = append(files, volumeservice.FileEntry{Name: base, Type: fileType})
			}
		}
	}

	return volumeservice.ListFolderResponse{
		Common:   ok(),
		IsVolume: isVolume(dir),
		Folders:  folders,
		Files:    files,
	}
}
