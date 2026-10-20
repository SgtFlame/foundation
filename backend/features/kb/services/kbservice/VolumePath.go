package kbservice

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// typeExtensions maps a content type to its file extension.
var typeExtensions = map[string]string{
	"markdown": ".md",
	"json":     ".json",
	"yaml":     ".yml",
}

// invalidSegmentChars are characters not allowed in a path segment on common OSes,
// plus '.' so that segments cannot be ".", ".." or carry their own extension.
const invalidSegmentChars = `/\:*?"<>|.`

// reservedSegmentNames are device names Windows does not allow as file or folder names.
var reservedSegmentNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}

// VolumePath builds an OS path of the form [root/][tenant/][user/]folder/.../folder<ext>.
type VolumePath struct {
	root      *string
	tenant    *string
	user      *string
	folders   []string
	extension string
	err       error
}

func NewVolumePath() *VolumePath {
	return &VolumePath{}
}

// Root returns a copy of vp rooted at the absolute path root, leaving vp unchanged.
// Setting the root of a path that already has one is reported by Build.
func (vp VolumePath) Root(root string) VolumePath {
	vp.folders = slices.Clone(vp.folders)
	switch {
	case vp.root != nil:
		vp.setErr(fmt.Errorf("kbservice: volume path root already set to %q", *vp.root))
	case !filepath.IsAbs(root):
		vp.setErr(fmt.Errorf("kbservice: volume path root %q is not absolute", root))
	default:
		root = filepath.Clean(root)
		vp.root = &root
	}
	return vp
}

func (vp *VolumePath) Tenant(tenant string) *VolumePath {
	if vp.setErr(validateSegment("tenant", tenant)) {
		vp.tenant = &tenant
	}
	return vp
}

func (vp *VolumePath) User(user string) *VolumePath {
	if vp.setErr(validateSegment("user", user)) {
		vp.user = &user
	}
	return vp
}

func (vp *VolumePath) Folder(folders ...string) *VolumePath {
	for _, folder := range folders {
		if !vp.setErr(validateSegment("folder", folder)) {
			return vp
		}
	}
	vp.folders = append(vp.folders, folders...)
	return vp
}

func (vp *VolumePath) Extension(ext string) *VolumePath {
	ext = strings.TrimPrefix(ext, ".")
	if vp.setErr(validateSegment("extension", ext)) {
		vp.extension = "." + ext
	}
	return vp
}

// Type sets the extension from a known content type; an unknown type is reported by Build.
func (vp *VolumePath) Type(t string) *VolumePath {
	ext, ok := typeExtensions[t]
	if !ok {
		vp.setErr(fmt.Errorf("kbservice: unknown volume path type %q", t))
		return vp
	}
	vp.extension = ext
	return vp
}

func (vp *VolumePath) Build() (string, error) {
	if vp.err != nil {
		return "", vp.err
	}

	var parts []string
	if vp.root != nil {
		parts = append(parts, *vp.root)
	}
	if vp.tenant != nil {
		parts = append(parts, *vp.tenant)
	}
	if vp.user != nil {
		parts = append(parts, *vp.user)
	}
	parts = append(parts, vp.folders...)

	return filepath.Join(parts...) + vp.extension, nil
}

// setErr records the first error seen and reports whether err was nil.
func (vp *VolumePath) setErr(err error) bool {
	if err != nil && vp.err == nil {
		vp.err = err
	}
	return err == nil
}

func validateSegment(kind, segment string) error {
	if segment == "" {
		return fmt.Errorf("kbservice: volume path %s is empty", kind)
	}
	if strings.TrimSpace(segment) != segment {
		return fmt.Errorf("kbservice: volume path %s %q has leading or trailing whitespace", kind, segment)
	}
	for _, r := range segment {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(invalidSegmentChars, r) {
			return fmt.Errorf("kbservice: volume path %s %q contains invalid character %q", kind, segment, r)
		}
	}
	if slices.Contains(reservedSegmentNames, strings.ToUpper(segment)) {
		return fmt.Errorf("kbservice: volume path %s %q is a reserved name", kind, segment)
	}
	return nil
}
