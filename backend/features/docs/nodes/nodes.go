// Package docsnodes holds the nodes the docs library app's graphs use to
// reach a Volume (forge 35a-2): Create page file, Remove page file and May
// use Volume. Writing files is a side effect, so the first two run in
// function, action, agent and trigger graphs, never in a command's
// decision; May use Volume is pure, for commands and queries to keep to a
// Volume's grants.
package docsnodes

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/docs"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
)

const source = "docs"

// The nodes' ids.
const (
	CreatePageFileID = "docs.createPageFile"
	RemovePageFileID = "docs.removePageFile"
	MayUseVolumeID   = "docs.mayUseVolume"
)

var effectKinds = []string{cognition.KindFunction, cognition.KindAgent, livenodes.KindAction, livenodes.KindTrigger}

var filePin = func(id, name, description string, optional bool) cognition.Pin {
	return cognition.Pin{ID: id, Name: name, Type: "json", Optional: optional, Description: description}
}

// Contribute registers the nodes; deps returns the Volumes once made.
func Contribute(reg *extensions.Registry, deps func() docs.Deps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: CreatePageFileID, Source: source, Order: 1200}, Title: "Create page file", Category: "Docs",
		Description: "Makes a page's document file, pages/<page>.doc.json in a Volume, as the run's principal (who needs volume.write on it): empty, or a copy of a template page's document. Run it before the page's record is made, so a page never names a file that does not exist (forge 35a-2).",
		Example:     "Create page file in the args' volume for a New id, copying the template's file, then send docs.page.add",
		Tags:        []string{"docs", "page", "volume", "file", "document"},
		Kinds:       effectKinds, Effects: true, External: true,
		Sample: cognition.Values{"file": map[string]any{"volumeId": "7340032000", "path": "pages/7340032001.doc.json"}},
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("volume", "Volume", "string", "The Volume's id"),
				cognition.DataPin("page", "Page", "string", "The new page's id"),
				filePin("from", "From", "A template page's file {volumeId, path} to copy; an empty document when unconnected", true)},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, ""),
				filePin("file", "File", "The new file {volumeId, path}, for the page's file field", false)},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			volume, _ := cognition.Get[string](in, "volume")
			page, _ := cognition.Get[string](in, "page")
			d := deps()
			var content []byte
			if from, ok := fileOf(in["from"]); ok {
				raw, err := d.ReadPageFile(x.ServiceArgs, from)
				if err != nil {
					return nil, "", err
				}
				content = raw
			}
			f, err := d.CreatePageFile(x.ServiceArgs, volume, page, content)
			if err != nil {
				return nil, "", err
			}
			return cognition.Values{"file": map[string]any{"volumeId": f.VolumeID, "path": f.Path}}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: RemovePageFileID, Source: source, Order: 1210}, Title: "Remove page file", Category: "Docs",
		Description: "Deletes a page's document file (pages/<page>.doc.json; no other file) for good, as the run's principal, who needs volume.write, or the docs app's own trigger (the purge of old trash). A file already gone is no error (forge 35a-2).",
		Example:     "Remove page file for each page trashed more than trashDays ago, then Delete record",
		Tags:        []string{"docs", "page", "volume", "file", "purge", "delete"},
		Kinds:       effectKinds, Effects: true, External: true,
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), filePin("file", "File", "The page's file {volumeId, path}", false)},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			f, ok := fileOf(in["file"])
			if !ok {
				return nil, "", fmt.Errorf("give the page's file {volumeId, path}")
			}
			if err := deps().RemovePageFile(x.ServiceArgs, f); err != nil {
				return nil, "", err
			}
			return cognition.Values{}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: MayUseVolumeID, Source: source, Order: 1220}, Title: "May use Volume", Category: "Docs",
		Description: "Whether the run's principal holds volume.read or volume.write on a Volume, the floor of its pages' access (forge 35a-2): a page command refuses without write, the tree query gives nothing without read.",
		Example:     "May use Volume write on the page's volume, then Reject when it is false",
		Tags:        []string{"docs", "volume", "access", "rbac", "permission"},
		Volatile:    true,
		Pins: cognition.Pins{
			In: []cognition.Pin{cognition.DataPin("volume", "Volume", "string", "The Volume's id"),
				{ID: "access", Name: "Access", Type: "string", Optional: true, Description: "read or write; read when empty"}},
			Out: []cognition.Pin{cognition.DataPin("allowed", "Allowed", "bool", "Whether the principal holds it")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			volume, _ := cognition.Get[string](in, "volume")
			access, _ := cognition.Get[string](in, "access")
			if access == "" {
				access = "read"
			}
			return cognition.Values{"allowed": docs.MayUse(x.Context(), x.Principal, volume, access)}, "", nil
		},
	})
}

// fileOf reads a volumeFile value.
func fileOf(v any) (docs.File, bool) {
	if v == nil {
		return docs.File{}, false
	}
	raw, _ := json.Marshal(v)
	var f docs.File
	if json.Unmarshal(raw, &f) != nil || f.VolumeID == "" || f.Path == "" {
		return docs.File{}, false
	}
	return f, true
}
