package volumeservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// MarkerFile is the file that makes a folder a volume. Folder names cannot
// contain '.', so it never collides with a folder.
const MarkerFile = ".volume.json"

// Marker is the content of a volume's MarkerFile.
type Marker struct {
	ID      uint64    `json:"id,string"`
	Created time.Time `json:"created"`
	// Mirror makes the Volume a read-only copy of a git repository's
	// folder (forge 24e): only the mirror's sync writes it, never a person.
	Mirror *Mirror `json:"mirror,omitempty"`
}

// Mirror names what a mirrored Volume copies.
type Mirror struct {
	Connection string `json:"connection"`
	Ref        string `json:"ref"`
	Path       string `json:"path,omitempty"`
}

// ReadMarker reads the marker of the volume at dir; the error wraps
// fs.ErrNotExist when dir is not a volume.
func ReadMarker(dir string) (Marker, error) {
	var marker Marker
	data, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err != nil {
		return marker, err
	}
	err = json.Unmarshal(data, &marker)
	return marker, err
}
