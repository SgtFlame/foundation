// Package kbtools contributes the knowledge base's tools
// (documentation/Tools.md): kb.search and kb.read, which cite what
// they find like retrieval does, and kb.createNote and kb.edit, which ask
// first. Paths are relative to the knowledge base root, with '/', as the
// citations name them ("veritas/notes.md").
package kbtools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/collabservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
	"github.com/SgtFlame/foundation/backend/features/kb/services/volumeservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/retrieval/services/retrievalservice"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Deps are what the tools call; they are created after the registry, so
// Contribute takes a function returning them.
type Deps struct {
	Root      string // the knowledge base root
	Retrieval retrievalservice.Service
	Volumes   volumeservice.Service
	Collab    collabservice.Service
}

// maxReadLines is the most lines kb.read returns at once.
const maxReadLines = 400

const source = "kb"

// register registers t, failing its calls from a tenant that has no Volumes.
func register(reg *extensions.Registry, t tools.Tool) {
	run := t.Run
	t.Run = func(call tools.Call) (tools.Result, error) {
		if !kbservice.InTenant(call.Principal) {
			return tools.Result{}, errors.New("Volumes are only available in the default tenant for now")
		}
		return run(call)
	}
	tools.Tools.Register(reg, t)
}

// Contribute registers the kb tools.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "kb.search", Order: 100, Source: source},
		Title:       "Search Volumes",
		Description: "Searches the user's Volumes (their knowledge base of markdown files) by meaning and keywords, and, where this conversation or agent allows, Chronicle memory. Returns the best passages, each with its source, path or memory entry, lines and a [n] to cite it by. Use it when the context you were given does not answer the question.",
		Input:       json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"description":"What to look for, in words likely to appear in the text"},"count":{"type":"integer","minimum":1,"maximum":20,"description":"How many passages, 6 by default"}},"required":["query"]}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Query string
				Count int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			req := searchArgs(call, args.Query, args.Count)
			if !kbservice.InTenant(call.Principal) && onlyLibrary(req.Sources) {
				return tools.Result{}, errors.New("Volumes are only available in the default tenant for now")
			}
			resp := deps().Retrieval.Retrieve(req)
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			content := map[string]any{"query": args.Query, "found": len(resp.Segments)}
			if resp.VectorSkipped {
				content["note"] = "Matched by keywords only: the semantic search was unavailable."
			}
			return tools.Result{Content: content, Segments: resp.Segments, Summary: fmt.Sprintf("Found %d %s for *%s*", len(resp.Segments), plural(len(resp.Segments), "passage", "passages"), markdownSafe(args.Query))}, nil
		},
	})

	register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "kb.read", Order: 110, Source: source},
		Title:       "Read a file",
		Description: fmt.Sprintf("Reads a markdown file or a document (.doc.json, read as its Markdown) in the user's Volumes, or a range of its lines (at most %d at once). A markdown file's text comes back exactly as stored, with a [n] to cite it by. Read a file before editing it.", maxReadLines),
		Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"The file's path, as a search result or citation names it, e.g. veritas/notes.md"},"startLine":{"type":"integer","minimum":1,"description":"The first line, from 1"},"endLine":{"type":"integer","minimum":1,"description":"The last line, inclusive"}},"required":["path"]}`),
		Effect:      tools.EffectRead,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Path               string
				StartLine, EndLine int
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			d := deps()
			_, rel, file, err := resolve(d.Root, args.Path, "markdown", "document")
			if err != nil {
				return tools.Result{}, err
			}
			if !volumeservice.May(call.ServiceArgs, d.Root, filepath.Dir(file), "read") {
				return tools.Result{}, fmt.Errorf("you may not read %q", rel)
			}
			content, err := os.ReadFile(file)
			if errors.Is(err, fs.ErrNotExist) {
				return tools.Result{}, fmt.Errorf("there is no file %q; search for it first", rel)
			}
			if err != nil {
				return tools.Result{}, fmt.Errorf("cannot read %q", rel)
			}
			// A document is read as its derived Markdown (forge 35a-1),
			// whose lines its citations name
			_, typ, _ := kbservice.SplitFile(filepath.Base(file))
			body, err := volumeservice.TextOf(typ, content)
			if err != nil {
				return tools.Result{}, fmt.Errorf("%q is not a readable document", rel)
			}
			lines := strings.SplitAfter(body, "\n")
			if n := len(lines); n > 0 && lines[n-1] == "" {
				lines = lines[:n-1]
			}
			first, last := max(args.StartLine, 1), args.EndLine
			if last == 0 || last > len(lines) {
				last = len(lines)
			}
			if last-first+1 > maxReadLines {
				last = first + maxReadLines - 1
			}
			if first > len(lines) && len(lines) > 0 {
				return tools.Result{}, fmt.Errorf("%q has only %d lines", rel, len(lines))
			}
			text := ""
			if len(lines) > 0 {
				text = strings.Join(lines[first-1:last], "")
			}
			sum := sha256.Sum256([]byte(text))
			span := fmt.Sprintf("lines %d–%d", first, last)
			seg := retrieval.Scored{Segment: retrieval.Segment{
				SourceID: "library", ID: fmt.Sprintf("%s#L%d-%d", rel, first, last), Key: hex.EncodeToString(sum[:]),
				Title: titleOf(rel) + " › " + span, Text: text,
				Location: retrieval.Location{Path: rel, StartLine: first, EndLine: last},
			}, Reason: "kb.read"}
			out := map[string]any{"path": rel, "totalLines": len(lines), "startLine": first, "endLine": last}
			if last < len(lines) {
				out["more"] = fmt.Sprintf("The file goes on to line %d; read from line %d for more.", len(lines), last+1)
			}
			return tools.Result{Content: out, Segments: []retrieval.Scored{seg}, Summary: fmt.Sprintf("Read *%s*, %s of %d", markdownSafe(titleOf(rel)), span, len(lines))}, nil
		},
	})

	register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "kb.createNote", Order: 120, Source: source},
		Title:       "Create a note",
		Description: "Creates a new markdown file in one of the user's Volumes, with the given content. The folder must exist inside a Volume, and the file must not. The user approves it first.",
		Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"The new file's path: an existing folder inside a Volume, then the name, e.g. veritas/meeting-notes.md. Names cannot contain dots."},"content":{"type":"string","description":"The file's markdown"}},"required":["path","content"]}`),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct{ Path, Content string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			d := deps()
			path, rel, _, err := resolve(d.Root, args.Path)
			if err != nil {
				return tools.Result{}, err
			}
			resp := d.Volumes.CreateFile(volumeservice.CreateFileArgs{ServiceArgs: call.ServiceArgs, Path: path, Type: "markdown", Content: args.Content})
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			return tools.Result{Content: map[string]any{"created": rel}, Summary: fmt.Sprintf("Created *%s*", markdownSafe(titleOf(rel)))}, nil
		},
	})

	register(reg, tools.Tool{
		Descriptor:  extensions.Descriptor{ID: "kb.edit", Order: 130, Source: source},
		Title:       "Edit a file",
		Description: "Edits a markdown file in the user's Volumes by replacing text. Each edit's find text must appear exactly once in the file, so copy it exactly from kb.read, with enough around it to be unique. Edits apply in order, each to the result of the ones before. If the user has the file open, they see the edit at once. The user approves it first.",
		Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"The file's path, e.g. veritas/notes.md"},"edits":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"find":{"type":"string","minLength":1,"description":"The exact text to replace, unique in the file"},"replace":{"type":"string","description":"What replaces it; empty deletes it"}},"required":["find","replace"]}}},"required":["path","edits"]}`),
		Effect:      tools.EffectWrite,
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Path  string
				Edits []collabservice.Edit
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			d := deps()
			path, rel, _, err := resolve(d.Root, args.Path)
			if err != nil {
				return tools.Result{}, err
			}
			resp := d.Collab.EditFile(collabservice.EditFileArgs{ServiceArgs: call.ServiceArgs, Path: path, Type: "markdown", Edits: args.Edits})
			if resp.Common.Code >= 300 {
				return tools.Result{}, errors.New(resp.Common.Message)
			}
			summary := fmt.Sprintf("Edited *%s* (%d %s)", markdownSafe(titleOf(rel)), len(args.Edits), plural(len(args.Edits), "change", "changes"))
			if resp.Live {
				summary += " in the open editor"
			}
			return tools.Result{Content: map[string]any{"edited": rel, "edits": len(args.Edits), "live": resp.Live}, Summary: summary}, nil
		},
	})
}

// searchArgs is how kb.search runs (forge 15a): the model gives only the
// query and how many; the sources, dials, Top-K and Volumes come from the
// options on the call's context (a chat round's RAG Search settings, an
// agent run's record), never from the model. Without options it searches
// the Volumes alone with the dials at 0, as it always has. The options'
// Top-K, when set, wins over the model's count.
func searchArgs(call tools.Call, query string, count int) retrievalservice.RetrieveArgs {
	out := retrievalservice.RetrieveArgs{ServiceArgs: call.ServiceArgs, Query: query, TopK: count, Sources: []string{"library"}}
	if o, ok := retrieval.OptionsFrom(call.Context()); ok {
		out.Sources, out.LexicalSemantic, out.ChronicleLibrary, out.VolumeIDs = o.Sources, o.LexicalSemantic, o.ChronicleLibrary, o.VolumeIDs
		out.Partitions, out.Collections = o.Partitions, o.Collections // forge 18f: Blackboards' branches and spaces
		if o.TopK > 0 {
			out.TopK = o.TopK
		}
	}
	// A workspace's unchecked Volumes are never searched (forge 16c)
	out.ExceptVolumeIDs = retrieval.ScopeFrom(call.Context()).ExceptVolumeIDs
	if out.TopK == 0 {
		out.TopK = 6
	}
	return out
}

// onlyLibrary tells whether sources name the Volumes and nothing else that
// could answer.
func onlyLibrary(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, s := range sources {
		if s != "library" && s != "none" {
			return false
		}
	}
	return true
}

// resolve turns a path relative to the root ("veritas/notes.md", the
// extension optional) into a volume path, its normalized form, the file,
// which must lie inside a volume, and its type, which must be one of
// types (markdown when none are given; a path without an extension is
// markdown).
func resolve(root, p string, types ...string) (kbservice.VolumePath, string, string, error) {
	if len(types) == 0 {
		types = []string{"markdown"}
	}
	p = strings.Trim(strings.TrimSpace(filepath.ToSlash(p)), "/")
	segments := strings.Split(p, "/")
	base, typ, ok := kbservice.SplitFile(segments[len(segments)-1])
	if !ok {
		p += ".md"
		segments = strings.Split(p, "/")
		base, typ, ok = kbservice.SplitFile(segments[len(segments)-1])
	}
	if !ok || len(segments) < 2 {
		return kbservice.VolumePath{}, "", "", fmt.Errorf("%q is not a markdown file's path inside a Volume, such as veritas/notes.md (names cannot contain dots)", p)
	}
	if !slices.Contains(types, typ) {
		return kbservice.VolumePath{}, "", "", fmt.Errorf("%q is a %s file; this tool takes only %s files", p, typ, strings.Join(types, " and "))
	}
	typed := kbservice.NewVolumePath().Folder(append(segments[:len(segments)-1:len(segments)-1], base)...).Type(typ).Root(root)
	file, err := typed.Build()
	if err != nil {
		return kbservice.VolumePath{}, "", "", fmt.Errorf("%q is not a valid path", p)
	}
	if !volumeservice.InsideVolume(root, filepath.Dir(file)) {
		return kbservice.VolumePath{}, "", "", fmt.Errorf("%q is not inside a Volume", p)
	}
	// The volume path without its type: the services set it
	bare := kbservice.NewVolumePath().Folder(append(segments[:len(segments)-1:len(segments)-1], base)...)
	return *bare, p, file, nil
}

// titleOf is a path without its extension.
func titleOf(rel string) string {
	dir, name := "", rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir, name = rel[:i+1], rel[i+1:]
	}
	if base, _, ok := kbservice.SplitFile(name); ok {
		return dir + base
	}
	return rel
}

// markdownSafe keeps text from closing the emphasis it is shown in.
func markdownSafe(s string) string {
	return strings.NewReplacer("*", "", "`", "", "\n", " ").Replace(s)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
