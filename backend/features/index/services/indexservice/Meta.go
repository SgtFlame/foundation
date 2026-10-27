package indexservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// MetaVersion changes when chunking changes, so every file is chunked again.
const MetaVersion = 1

// Meta is the sidecar index record of one volume file, kept next to it in
// MetaFile(file). A chunk whose Hash is unchanged keeps its embedding.
type Meta struct {
	Version    int         `json:"version"`
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	FileHash   string      `json:"fileHash"`
	IndexedAt  time.Time   `json:"indexedAt"`
	Chunks     []MetaChunk `json:"chunks"`
}

type MetaChunk struct {
	Hash      string `json:"hash"` // SHA-256 of the chunk text, first 128 bits in hex
	Start     int    `json:"start"`
	End       int    `json:"end"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Heading   string `json:"heading,omitempty"`
	Head      string `json:"head"`
	Tail      string `json:"tail"`
}

// MetaFile is the sidecar of file: ".<name>.index.json" in the same folder.
// Dotted names are never listed, like the volume marker.
func MetaFile(file string) string {
	return filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".index.json")
}

// SourceFile is the file a sidecar belongs to, or "" when name is not a sidecar.
func SourceFile(meta string) string {
	base := filepath.Base(meta)
	const suffix = ".index.json"
	if len(base) <= 1+len(suffix) || base[0] != '.' || base[len(base)-len(suffix):] != suffix {
		return ""
	}
	return filepath.Join(filepath.Dir(meta), base[1:len(base)-len(suffix)])
}

// ReadMeta reads file's sidecar; the error wraps fs.ErrNotExist when there is none.
func ReadMeta(file string) (Meta, error) {
	var meta Meta
	data, err := os.ReadFile(MetaFile(file))
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// WriteMeta writes file's sidecar atomically through a dotted temp file.
func WriteMeta(file string, meta Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	target := MetaFile(file)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
