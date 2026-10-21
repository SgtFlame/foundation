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
