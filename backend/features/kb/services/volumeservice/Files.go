package volumeservice

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the kb feature's logging and error builder.
var Log = logging.In("kb")

// Resolve builds path under root, creating root if it is missing. With
// requireFolder, a path naming no folder (the root itself, or a tenant or user
// directory) is rejected.
func Resolve(args features.ServiceArgs, root string, path kbservice.VolumePath, requireFolder bool) (string, *features.ServiceResponse) {
	if requireFolder && len(path.Folders()) == 0 {
		return "", args.Fail(http.StatusBadRequest, Log.Code("no_folder").New("path names no folder"))
	}
	rooted := path.Root(root)
	dir, err := rooted.Build()
	if err != nil {
		return "", args.Fail(http.StatusBadRequest, Log.Code("invalid_path").Wrap(err))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", args.Fail(http.StatusInternalServerError, Log.Wrap(err))
	}
	return dir, nil
}

// StatFolder checks that dir exists and is a directory.
func StatFolder(args features.ServiceArgs, root, dir string) *features.ServiceResponse {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return args.Fail(http.StatusNotFound, Log.Code("folder_not_found").Errorf("folder %q does not exist", Rel(root, dir)))
	case err != nil:
		return args.Fail(http.StatusInternalServerError, Log.Wrap(err))
	case !info.IsDir():
		return args.Fail(http.StatusBadRequest, Log.Code("not_a_folder").Errorf("%q is not a folder", Rel(root, dir)))
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
