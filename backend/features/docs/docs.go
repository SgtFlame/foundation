// Package docs is the Go side of the docs library app (forge 35a-2): the
// nodes its graphs use to reach a Volume (Create page file, Remove page
// file, May use Volume) and the hook that marks a page saved when its
// document file is written. The app itself (entity types, graphs,
// manifest) is docsapp, in features/docs/app.
//
// A page's document is the Volume file pages/<pageId>.doc.json: files stay
// flat and the tree is held in the page records (35a-2 question 1), so
// moving a page never renames a file.
package docs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

// AppID is the docs library app's id.
const AppID = "docs"

// EntityPage is the page entity type's id.
const EntityPage = "docsPage"

// PagesFolder is the folder of a Volume that holds the pages' documents.
const PagesFolder = "pages"

var log = logging.In("docs")

// Deps are what the docs nodes and hooks need from the server.
type Deps struct {
	Root    string // the knowledge base root
	Volumes volumeservice.Service
}

// File is a volumeFile value: a file in a Volume, path relative to it.
type File struct {
	VolumeID string `json:"volumeId"`
	Path     string `json:"path"`
}

// PageFile is the document file of a page in a Volume.
func PageFile(volumeID, pageID string) File {
	return File{VolumeID: volumeID, Path: PagesFolder + "/" + pageID + ".doc.json"}
}

// VolumeResource is a Volume's access path in p's tenant.
func VolumeResource(p features.Principal, volumeID string) string {
	return authz.Tenant(p) + "/volume:" + volumeID
}

// MayUse tells whether p holds volume.<access> ("read" or "write") on the
// Volume: the floor of every page's access (35a-2 › Access).
func MayUse(ctx context.Context, p features.Principal, volumeID, access string) bool {
	if !validID(volumeID) || (access != "read" && access != "write") {
		return false
	}
	if p.Kind == features.PrincipalApp && p.App == AppID {
		return true // the docs app's own triggers (the purge)
	}
	return authz.Check(ctx, p, "volume."+access, VolumeResource(p, volumeID)).Allowed
}

// VolumeDir finds the folder of the Volume with this id under root, by its
// marker; Volumes do not nest.
func VolumeDir(root, volumeID string) (string, error) {
	id, err := strconv.ParseUint(volumeID, 10, 64)
	if err != nil || root == "" {
		return "", fmt.Errorf("there is no Volume %q", volumeID)
	}
	found := ""
	_ = filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || found != "" {
			return filepath.SkipDir
		}
		if dir != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if m, err := volumeservice.ReadMarker(dir); err == nil {
			if m.ID == id {
				found = dir
			}
			return filepath.SkipDir
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("there is no Volume %q", volumeID)
	}
	return found, nil
}

// folders are dir's folders from root.
func folders(root, dir string) []string {
	rel, _ := filepath.Rel(root, dir)
	if rel == "." || rel == "" {
		return nil
	}
	return strings.Split(filepath.ToSlash(rel), "/")
}

// CreatePageFile makes a page's document file through the volume service as
// args' principal (who needs volume.write), holding content (a document
// file or a bare ProseMirror doc; an empty document when empty), making
// the pages folder first when the Volume has none.
func (d Deps) CreatePageFile(args features.ServiceArgs, volumeID, pageID string, content []byte) (File, error) {
	if !validID(pageID) {
		return File{}, fmt.Errorf("%q is not a page id", pageID)
	}
	if d.Volumes == nil {
		return File{}, errors.New("there are no Volumes here")
	}
	dir, err := VolumeDir(d.Root, volumeID)
	if err != nil {
		return File{}, err
	}
	base := folders(d.Root, dir)
	if _, err := os.Stat(filepath.Join(dir, PagesFolder)); errors.Is(err, fs.ErrNotExist) {
		resp := d.Volumes.MakeFolder(volumeservice.MakeFolderArgs{ServiceArgs: args, Path: *kbservice.NewVolumePath().Folder(append(base, PagesFolder)...)})
		if resp.Common.Code >= 300 && resp.Common.ErrorCode != "folder_exists" {
			return File{}, errors.New(resp.Common.Message)
		}
	}
	path := kbservice.NewVolumePath().Folder(append(base, PagesFolder, pageID)...)
	resp := d.Volumes.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: args, Path: *path, Type: "document", Content: string(content)})
	if resp.Common.Code >= 300 {
		return File{}, errors.New(resp.Common.Message)
	}
	return PageFile(volumeID, pageID), nil
}

// ReadPageFile reads a page's document file (a template's) as args'
// principal, who needs volume.read on its Volume.
func (d Deps) ReadPageFile(args features.ServiceArgs, f File) ([]byte, error) {
	file, err := d.pagePath(f)
	if err != nil {
		return nil, err
	}
	if !MayUse(args.Context(), args.Principal, f.VolumeID, "read") {
		return nil, fmt.Errorf("you may not read Volume %s", f.VolumeID)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("the page's document cannot be read")
	}
	return raw, nil
}

// RemovePageFile deletes a page's document file (the purge of a trashed
// page), as args' principal, who needs volume.write or is the docs app.
// A file already gone is no error, so a purge run twice changes nothing.
func (d Deps) RemovePageFile(args features.ServiceArgs, f File) error {
	file, err := d.pagePath(f)
	if err != nil {
		return err
	}
	if !MayUse(args.Context(), args.Principal, f.VolumeID, "write") {
		return fmt.Errorf("you may not change Volume %s", f.VolumeID)
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return log.Wrap(err)
	}
	return nil
}

// pagePath is the file of a page's document, refusing any other file.
func (d Deps) pagePath(f File) (string, error) {
	id, ok := PageIDOf(f.Path)
	if !ok {
		return "", fmt.Errorf("%q is not a page's document", f.Path)
	}
	dir, err := VolumeDir(d.Root, f.VolumeID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, PagesFolder, id+".doc.json"), nil
}

// PageIDOf is the page id of a page document's path within its Volume
// (pages/<id>.doc.json).
func PageIDOf(path string) (string, bool) {
	rest, ok := strings.CutPrefix(filepath.ToSlash(path), PagesFolder+"/")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, ".doc.json")
	if !ok || !validID(id) {
		return "", false
	}
	return id, true
}

// SavedPage is the page whose document file an absolute path is, with the
// file's hash: "", false for any other file.
func SavedPage(root, file string) (string, string, bool) {
	if filepath.Base(filepath.Dir(file)) != PagesFolder || !volumeservice.InsideVolume(root, filepath.Dir(file)) {
		return "", "", false
	}
	id, ok := PageIDOf(PagesFolder + "/" + filepath.Base(file))
	if !ok {
		return "", "", false
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", "", false
	}
	sum := sha256.Sum256(raw)
	return id, hex.EncodeToString(sum[:]), true
}

// validID is a record id: digits only.
func validID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
