package gitnodes

import (
	"encoding/base64"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/git"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
)

// MirrorStore keeps mirrored Volumes: volumeservice.Mirrors.
type MirrorStore interface {
	Ensure(args features.ServiceArgs, name string, mirror volumeservice.Mirror) (uint64, error)
	Write(args features.ServiceArgs, name, path string, data []byte) error
	Remove(args features.ServiceArgs, name, path string) error
	Files(args features.ServiceArgs, name string) ([]string, error)
}

// MaxMirrorFile is the largest file a mirror copies (256 KB, the plan's
// question 6): bigger files are skipped.
const MaxMirrorFile = 256 << 10

// MaxMirrorFiles bounds the files one mirror holds.
const MaxMirrorFiles = 20000

// DefaultIgnore are the folders a mirror skips, vendored and generated
// code, wherever they are in the tree.
var DefaultIgnore = []string{"node_modules", "vendor", "dist", "build", "public", "resources", "target", "out"}

// SyncInput is one mirror sync.
type SyncInput struct {
	Connection string   `json:"connection"`
	Ref        string   `json:"ref"`
	Path       string   `json:"path,omitempty"`
	Volume     string   `json:"volume"`
	From       string   `json:"from,omitempty"`
	Ignore     []string `json:"ignore,omitempty"`
}

// SyncResult is what a sync did.
type SyncResult struct {
	SHA      string `json:"sha"`
	VolumeID uint64 `json:"volumeId,string"`
	Written  int    `json:"written"`
	Removed  int    `json:"removed"`
	Skipped  int    `json:"skipped"`
	Full     bool   `json:"full"`
}

func (in SyncInput) prefix() string {
	p := strings.Trim(in.Path, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// local is a repository path's path in the Volume, ok false when it is
// outside the mirrored folder or skipped.
func (in SyncInput) local(path string) (string, bool) {
	pre := in.prefix()
	if !strings.HasPrefix(path, pre) {
		return "", false
	}
	rel := strings.TrimPrefix(path, pre)
	ignore := in.Ignore
	if ignore == nil {
		ignore = DefaultIgnore
	}
	segs := strings.Split(rel, "/")
	for _, s := range segs[:len(segs)-1] {
		if slices.Contains(ignore, s) {
			return "", false
		}
	}
	return rel, volumeservice.Mirrorable(rel)
}

// Sync brings a mirrored Volume to a ref's head: with From (the commit
// last synced) only the paths compare names change; without it, or when
// From is out of reach, every file is written and the Volume's other files
// removed. Writing a file that is already there changes nothing, so a
// retried sync is harmless.
func (d Deps) Sync(args features.ServiceArgs, in SyncInput) (SyncResult, *connectors.Error) {
	var res SyncResult
	if d.Mirrors == nil {
		return res, connectors.Errorf(connectors.CodeUnavailable, "there are no mirrored Volumes here")
	}
	if in.Connection == "" || in.Volume == "" {
		return res, connectors.Errorf(connectors.CodeInvalid, "a mirror needs a connection and a Volume name")
	}
	dir, err := git.CleanDir(in.Path)
	if err != nil {
		return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
	}
	in.Path = dir
	id, err := d.Mirrors.Ensure(args, in.Volume, volumeservice.Mirror{Connection: in.Connection, Ref: in.Ref, Path: in.Path})
	if err != nil {
		return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
	}
	res.VolumeID = id
	tree, e := d.Tree(args, in.Connection, in.Ref, in.Path, true, MaxMirrorFiles)
	if e != nil {
		return res, e
	}
	res.SHA = tree.SHA
	if in.From == tree.SHA {
		return res, nil
	}
	sizes := map[string]int64{}
	for _, en := range tree.Entries {
		if en.Kind == "file" {
			sizes[en.Path] = en.Size
		}
	}
	var write, remove []string
	if in.From != "" {
		var cmp git.Comparison
		e := d.call(args, git.RepoRef, in.Connection, "compare", map[string]any{"base": in.From, "head": tree.SHA}, &cmp)
		switch {
		case e == nil:
			for _, c := range cmp.Changes {
				if c.Status == "deleted" {
					remove = append(remove, c.Path)
				} else {
					write = append(write, c.Path)
				}
			}
		case e.Code == connectors.CodeNotFound:
			in.From = "" // the last synced commit is gone (a force push): copy it all
		default:
			return res, e
		}
	}
	if in.From == "" {
		res.Full = true
		for p := range sizes {
			write = append(write, p)
		}
		have, err := d.Mirrors.Files(args, in.Volume)
		if err != nil {
			return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
		}
		keep := map[string]bool{}
		for p := range sizes {
			if rel, ok := in.local(p); ok && sizes[p] <= MaxMirrorFile {
				keep[rel] = true
			}
		}
		for _, rel := range have {
			if !keep[rel] {
				if err := d.Mirrors.Remove(args, in.Volume, rel); err != nil {
					return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
				}
				res.Removed++
			}
		}
	}
	slices.Sort(write)
	for _, p := range remove {
		if rel, ok := in.local(p); ok {
			if err := d.Mirrors.Remove(args, in.Volume, rel); err != nil {
				return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
			}
			res.Removed++
		}
	}
	for _, p := range write {
		rel, ok := in.local(p)
		if !ok {
			if strings.HasPrefix(p, in.prefix()) {
				res.Skipped++
			}
			continue
		}
		if sizes[p] > MaxMirrorFile {
			// a file grown past the limit leaves the mirror
			_ = d.Mirrors.Remove(args, in.Volume, rel)
			res.Skipped++
			continue
		}
		f, e := d.ReadFile(args, in.Connection, tree.SHA, p)
		if e != nil {
			return res, e
		}
		content, _ := f["content"].(string)
		data := []byte(content)
		if f["encoding"] == "base64" {
			if data, err = base64.StdEncoding.DecodeString(content); err != nil {
				return res, connectors.Errorf(connectors.CodeUnavailable, "%s came back as bad base64", p)
			}
		}
		if err := d.Mirrors.Write(args, in.Volume, rel, data); err != nil {
			return res, connectors.Errorf(connectors.CodeInvalid, "%v", err)
		}
		res.Written++
	}
	return res, nil
}
