package volumeservice

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// Resolve builds path under root, creating root if it is missing. With
// requireFolder, a path naming no folder (the root itself, or a tenant or user
// directory) is rejected.
func Resolve(root string, path kbservice.VolumePath, requireFolder bool) (string, *features.ServiceResponse) {
	if requireFolder && len(path.Folders()) == 0 {
		return "", &features.ServiceResponse{Code: http.StatusBadRequest, Message: "path names no folder"}
	}
	rooted := path.Root(root)
	dir, err := rooted.Build()
	if err != nil {
		return "", &features.ServiceResponse{Code: http.StatusBadRequest, Message: err.Error()}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", &features.ServiceResponse{Code: http.StatusInternalServerError, Message: err.Error()}
	}
	return dir, nil
}

// StatFolder checks that dir exists and is a directory.
func StatFolder(root, dir string) *features.ServiceResponse {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Fail(http.StatusNotFound, "folder %q does not exist", Rel(root, dir))
	case err != nil:
		return Fail(http.StatusInternalServerError, "%s", err)
	case !info.IsDir():
		return Fail(http.StatusBadRequest, "%q is not a folder", Rel(root, dir))
	}
	return nil
}

// InsideVolume reports whether dir is a volume or lies within one.
func InsideVolume(root, dir string) bool {
	for ; ; dir = filepath.Dir(dir) {
		if IsVolume(dir) {
			return true
		}
		if dir == root || dir == filepath.Dir(dir) {
			return false
		}
	}
}

// IsVolume reports whether dir holds a volume marker.
func IsVolume(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, MarkerFile))
	return err == nil
}

// Rel names p relative to root, so that messages do not expose server paths.
func Rel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}

func Fail(code uint16, format string, args ...any) *features.ServiceResponse {
	return &features.ServiceResponse{Code: code, Message: fmt.Sprintf(format, args...)}
}

func OK() features.ServiceResponse {
	return features.ServiceResponse{Code: http.StatusOK}
}
