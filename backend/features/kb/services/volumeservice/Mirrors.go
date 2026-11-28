package volumeservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// MirrorsFolder is the folder under the knowledge base root that holds the
// mirrored Volumes (forge 24e).
const MirrorsFolder = "mirrors"

var mirrorName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Mirrors writes the Volumes that mirror git repositories (forge 24e):
// read-only to people (Check refuses every write in them), written only
// here, by background work and apps' mirror syncs, each write announced on
// Events so the index follows.
type Mirrors struct {
	Root   string
	Events FileEvents
}

// ErrMirrorAccess is what a person's (or an agent's) principal gets.
var ErrMirrorAccess = errors.New("only a repository mirror's sync writes its Volume")

func (m Mirrors) allowed(p features.Principal) error {
	if p.Kind != features.PrincipalService && p.Kind != features.PrincipalApp {
		return ErrMirrorAccess
	}
	if p.TenantID != features.LocalTenantID {
		return kbservice.ErrOtherTenant()
	}
	return nil
}

// Dir is the mirrored Volume's folder.
func (m Mirrors) Dir(name string) string { return filepath.Join(m.Root, MirrorsFolder, name) }

// Ensure makes the mirrored Volume name (a folder under mirrors/) for
// mirror, or gives the one already there when it mirrors the same thing,
// so a retried sync makes one Volume. A folder that is another Volume, or
// mirrors something else, is refused.
func (m Mirrors) Ensure(args features.ServiceArgs, name string, mirror Mirror) (uint64, error) {
	if err := m.allowed(args.Principal); err != nil {
		return 0, err
	}
	if !mirrorName.MatchString(name) {
		return 0, fmt.Errorf("a mirror's Volume name is lower-case letters, digits, - and _: %q", name)
	}
	dir := m.Dir(name)
	if marker, err := ReadMarker(dir); err == nil {
		if marker.Mirror == nil || *marker.Mirror != mirror {
			return 0, fmt.Errorf("the Volume %s/%s already holds something else", MirrorsFolder, name)
		}
		return marker.ID, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	marker := Marker{ID: features.NewTSID(), Created: time.Now().UTC(), Mirror: &mirror}
	data, _ := json.MarshalIndent(marker, "", "  ")
	f, err := os.OpenFile(filepath.Join(dir, MarkerFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return m.Ensure(args, name, mirror) // a racing sync made it
	}
	if err != nil {
		return 0, err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return 0, err
	}
	return marker.ID, nil
}

// MirrorOf reads a mirrored Volume's marker.
func (m Mirrors) MirrorOf(name string) (Marker, bool) {
	marker, err := ReadMarker(m.Dir(name))
	return marker, err == nil && marker.Mirror != nil
}

// target is the file path inside the mirrored Volume, checked: no dotted
// or empty segments (dotted entries are the Volume's own), so a mirrored
// path can never reach the marker, a sidecar or outside the folder.
func (m Mirrors) target(name, path string) (string, error) {
	if _, ok := m.MirrorOf(name); !ok {
		return "", fmt.Errorf("there is no mirrored Volume %s/%s", MirrorsFolder, name)
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if s == "" || strings.HasPrefix(s, ".") || (i < len(segs)-1 && strings.Contains(s, ".")) {
			return "", fmt.Errorf("%q cannot be mirrored: folders cannot hold '.', and dotted names are the Volume's own", path)
		}
	}
	return filepath.Join(m.Dir(name), filepath.FromSlash(path)), nil
}

// Mirrorable tells whether a repository path can be kept in a mirrored
// Volume (see target).
func Mirrorable(path string) bool {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if s == "" || strings.HasPrefix(s, ".") || (i < len(segs)-1 && strings.Contains(s, ".")) {
			return false
		}
	}
	return true
}

// Write writes one file into the mirrored Volume, atomically, and
// announces it.
func (m Mirrors) Write(args features.ServiceArgs, name, path string, data []byte) error {
	if err := m.allowed(args.Principal); err != nil {
		return err
	}
	file, err := m.target(name, path)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(file); err == nil && string(old) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".mirror-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), file)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if m.Events != nil {
		m.Events.Publish(FileEvent{Op: FileChanged, File: file})
	}
	return nil
}

// Remove deletes one file from the mirrored Volume (and the folders it
// leaves empty), and announces it so the index forgets it.
func (m Mirrors) Remove(args features.ServiceArgs, name, path string) error {
	if err := m.allowed(args.Principal); err != nil {
		return err
	}
	file, err := m.target(name, path)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_ = os.Remove(sidecarOf(file))
	for dir := filepath.Dir(file); dir != m.Dir(name); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	if m.Events != nil {
		m.Events.Publish(FileEvent{Op: FileChanged, File: file})
	}
	return nil
}

func sidecarOf(file string) string {
	return filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".index.json")
}

// Files lists the mirrored Volume's files, by path from its folder.
func (m Mirrors) Files(args features.ServiceArgs, name string) ([]string, error) {
	if err := m.allowed(args.Principal); err != nil {
		return nil, err
	}
	if _, ok := m.MirrorOf(name); !ok {
		return nil, fmt.Errorf("there is no mirrored Volume %s/%s", MirrorsFolder, name)
	}
	root := m.Dir(name)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}
